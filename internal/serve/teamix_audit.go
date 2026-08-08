package serve

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/auditlog"
)

// maxAuditRecords 单次查询返回的最大记录数（最新在前）。
const maxAuditRecords = 500

// handleAuditLogs 查询 AI 调用审计日志（仅 architect 可见，泄露审计）。
// 参数：user（默认全部用户）、date（YYYY-MM-DD，默认全部日期）、
// outbound=true（只看出了网的）、alert=true（只看有告警的）。
func (ts *TeamixServer) handleAuditLogs(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可查看 AI 审计日志", http.StatusForbidden)
		return
	}
	if ts.auditWriter == nil {
		writeJSON(w, map[string]any{"records": []auditlog.Record{}})
		return
	}
	q := r.URL.Query()
	filterUser := q.Get("user")
	date := q.Get("date")
	onlyOutbound := q.Get("outbound") == "true"
	onlyAlert := q.Get("alert") == "true"

	records, err := ts.readAuditLogs(filterUser, date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var out []auditlog.Record
	for _, rec := range records {
		if onlyOutbound && !rec.Outbound.Sent {
			continue
		}
		if onlyAlert && len(rec.Alerts) == 0 {
			continue
		}
		out = append(out, rec)
		if len(out) >= maxAuditRecords {
			break
		}
	}
	if out == nil {
		out = []auditlog.Record{}
	}
	writeJSON(w, map[string]any{"records": out, "total": len(out)})
}

// auditReportDay 单日聚合。
type auditReportDay struct {
	Date        string `json:"date"`
	Total       int    `json:"total"`
	Outbound    int    `json:"outbound"`    // 出了网的外部调用
	Critical    int    `json:"critical"`    // [critical] 告警数（含事故/闭环）
	ClosedLoop  int    `json:"closed_loop"` // 闭环检测命中数
}

// handleAuditReport 审计周报（仅 architect）：按天聚合最近 N 天（默认 7）
// 的 AI 调用记录——出网/内部次数、致命告警、闭环命中。泄露审计日常视图。
func (ts *TeamixServer) handleAuditReport(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可查看审计周报", http.StatusForbidden)
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	records, err := ts.readAuditLogs("", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	byDay := map[string]*auditReportDay{}
	var order []string
	for _, rec := range records {
		if rec.Time.Before(cutoff) {
			continue
		}
		key := rec.Time.Format("2006-01-02")
		d, ok := byDay[key]
		if !ok {
			d = &auditReportDay{Date: key}
			byDay[key] = d
			order = append(order, key)
		}
		d.Total++
		if rec.Outbound.Sent {
			d.Outbound++
		}
		for _, a := range rec.Alerts {
			if strings.HasPrefix(a, "[critical]") {
				d.Critical++
				if strings.Contains(a, "closed_loop") {
					d.ClosedLoop++
				}
			}
		}
	}
	sort.Strings(order)
	out := make([]auditReportDay, 0, len(order))
	totals := auditReportDay{Date: "合计"}
	for _, k := range order {
		d := byDay[k]
		out = append(out, *d)
		totals.Total += d.Total
		totals.Outbound += d.Outbound
		totals.Critical += d.Critical
		totals.ClosedLoop += d.ClosedLoop
	}
	writeJSON(w, map[string]any{"days": out, "totals": totals})
}

// readAuditLogs 读取审计目录下的 JSONL 记录，按时间倒序（最新在前）。
// user 为空 = 全部用户；date 为空 = 全部日期。
func (ts *TeamixServer) readAuditLogs(user, date string) ([]auditlog.Record, error) {
	dir := ts.auditDir()
	var out []auditlog.Record
	userDirs := []string{user}
	if user == "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return out, nil
			}
			return nil, err
		}
		userDirs = userDirs[:0]
		for _, e := range entries {
			if e.IsDir() {
				userDirs = append(userDirs, e.Name())
			}
		}
	}
	for _, ud := range userDirs {
		if ud == "" {
			continue
		}
		userDir := filepath.Join(dir, ud)
		files := []string{date}
		if date == "" {
			entries, err := os.ReadDir(userDir)
			if err != nil {
				continue
			}
			files = files[:0]
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					files = append(files, e.Name())
				}
			}
		}
		for _, fname := range files {
			fname = strings.TrimSuffix(fname, ".jsonl")
			recs, err := readJSONL(filepath.Join(userDir, fname+".jsonl"))
			if err != nil {
				continue
			}
			out = append(out, recs...)
		}
	}
	// 按时间倒序（最新在前）
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}


// readJSONL 逐行解析一个审计文件。
func readJSONL(path string) ([]auditlog.Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []auditlog.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec auditlog.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行跳过（不阻断查询）
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// auditUserDay 单用户单日 token 用量（周报统计）。
type auditUserDay struct {
	Date     string `json:"date"`
	Tokens   int    `json:"tokens"` // in + out
	In       int    `json:"in"`
	Out      int    `json:"out"`
	Calls    int    `json:"calls"`
	Outbound int    `json:"outbound"` // 出了网的外部调用
}

// auditUserStat 单用户近 N 天汇总（周报一行）。
type auditUserStat struct {
	User     string         `json:"user"`
	Tokens   int            `json:"tokens"`
	In       int            `json:"in"`
	Out      int            `json:"out"`
	Calls    int            `json:"calls"`
	Outbound int            `json:"outbound"`
	Critical int            `json:"critical"` // [critical] 告警数
	Daily    []auditUserDay `json:"daily"`    // 按 days 序列对齐
}

// handleAuditStats 近 N 天（默认 7）per-user token 用量统计（仅架构师）：
// 每天每个用户的 token 用量（in/out）、调用数、出网数、致命告警数。
// 返回 days 日期序列 + users（按总 token 降序，daily 对齐 days）+ totals（按天合计，供柱状图）。
func (ts *TeamixServer) handleAuditStats(w http.ResponseWriter, r *http.Request, u *userSession) {
	if !ts.isArchitect(u) {
		http.Error(w, "仅架构师可查看 AI 审计统计", http.StatusForbidden)
		return
	}
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	records, err := ts.readAuditLogs("", "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dayList, users, totals := aggregateAuditStats(records, days)
	if users == nil {
		users = []auditUserStat{}
	}
	writeJSON(w, map[string]any{"days": dayList, "users": users, "totals": totals})
}

// aggregateAuditStats 纯聚合：把审计记录按 用户×天 归并为近 days 天统计。
// 返回日期序列（含今天，旧→新）、按总 token 降序的用户列表、按天合计（柱状图）。
func aggregateAuditStats(records []auditlog.Record, days int) ([]string, []auditUserStat, []auditUserDay) {
	cutoff := time.Now().AddDate(0, 0, -days)
	// 日期序列：含今天在内的最近 days 天（旧→新）
	dayList := make([]string, 0, days)
	dayIdx := make(map[string]int, days)
	for i := days - 1; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dayIdx[d] = len(dayList)
		dayList = append(dayList, d)
	}
	type acc struct{ tokens, in, out, calls, outbound int }
	userDaily := map[string][]acc{}
	userCritical := map[string]int{}
	for _, rec := range records {
		if rec.Time.Before(cutoff) {
			continue
		}
		idx, ok := dayIdx[rec.Time.Format("2006-01-02")]
		if !ok {
			continue
		}
		user := rec.User
		if user == "" {
			user = "_system"
		}
		d := userDaily[user]
		if d == nil {
			d = make([]acc, len(dayList))
		}
		tk := rec.Cost.InTokens + rec.Cost.OutTokens
		d[idx].tokens += tk
		d[idx].in += rec.Cost.InTokens
		d[idx].out += rec.Cost.OutTokens
		d[idx].calls++
		if rec.Outbound.Sent {
			d[idx].outbound++
		}
		for _, a := range rec.Alerts {
			if strings.HasPrefix(a, "[critical]") {
				userCritical[user]++
				break
			}
		}
		userDaily[user] = d
	}
	users := make([]auditUserStat, 0, len(userDaily))
	for user, d := range userDaily {
		st := auditUserStat{User: user, Daily: make([]auditUserDay, len(dayList))}
		for i := range dayList {
			st.Daily[i] = auditUserDay{Date: dayList[i], Tokens: d[i].tokens, In: d[i].in, Out: d[i].out, Calls: d[i].calls, Outbound: d[i].outbound}
			st.Tokens += d[i].tokens
			st.In += d[i].in
			st.Out += d[i].out
			st.Calls += d[i].calls
			st.Outbound += d[i].outbound
		}
		st.Critical = userCritical[user]
		users = append(users, st)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Tokens > users[j].Tokens })
	// 按天合计（柱状图数据源）
	totals := make([]auditUserDay, len(dayList))
	for _, st := range users {
		for i := range dayList {
			totals[i].Date = dayList[i]
			totals[i].Tokens += st.Daily[i].Tokens
			totals[i].In += st.Daily[i].In
			totals[i].Out += st.Daily[i].Out
			totals[i].Calls += st.Daily[i].Calls
			totals[i].Outbound += st.Daily[i].Outbound
		}
	}
	return dayList, users, totals
}
