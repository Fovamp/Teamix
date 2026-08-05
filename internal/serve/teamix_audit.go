package serve

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// readAuditLogs 读取审计目录下的 JSONL 记录，按时间倒序（最新在前）。
// user 为空 = 全部用户；date 为空 = 全部日期。
func (ts *TeamixServer) readAuditLogs(user, date string) ([]auditlog.Record, error) {
	dir := auditDirOf(ts)
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

func auditDirOf(ts *TeamixServer) string {
	if ts.auditWriter != nil {
		// auditWriter 的 dir 不可直接读（私有）；从默认配置取——serve 初始化时
		// 用的是 globalCfg.Audit.Dir，回退默认值。
	}
	dir := ".teamix/logs/ai-audit"
	if ts.globalCfg != nil && ts.globalCfg.Config != nil && ts.globalCfg.Config.Audit.Dir != "" {
		dir = ts.globalCfg.Config.Audit.Dir
	}
	return filepath.Join(ts.workspaceRoot, dir)
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
