package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	reasonixAgent "reasonix/internal/agent"
	"reasonix/internal/config"
)

// sessionSummary 是"会话总结"面板里的一条记录：AI 生成的、面向人阅读的
// 会话摘要。与 /summarize（SummarizeUpTo/From，会改写会话消息日志的压缩操作）
// 不同：这里生成后完全不改动会话本身。Title 为总结首行提炼的标题；
// SessionID/SessionTitle 标注这条总结来自哪个会话（供前端箭头标注来源）。
type sessionSummary struct {
	ID           string    `json:"id"`
	Time         time.Time `json:"time"`
	Title        string    `json:"title,omitempty"`
	Content      string    `json:"content"`
	SessionID    string    `json:"sessionId,omitempty"`
	SessionTitle string    `json:"sessionTitle,omitempty"`
}

// summaryDir 返回会话总结的存储根目录（<workspaceRoot>/.teamix/summaries/）。
func (ts *TeamixServer) summaryDir() string {
	base := ts.workspaceRoot
	if base == "" {
		base = config.ReasonixHomeDir()
	}
	dir := filepath.Join(base, ".teamix", "summaries")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// summaryFile 返回某用户某会话的总结文件。sessionID 来自 SessionPath 的文件名，
// 仍做路径穿越防御：结果必须落在 summaryDir 之内。
func (ts *TeamixServer) summaryFile(user, sessionID string) string {
	userDir := filepath.Join(ts.summaryDir(), user)
	_ = os.MkdirAll(userDir, 0o755)
	p := filepath.Join(userDir, sessionID+".json")
	base := filepath.Clean(ts.summaryDir()) + string(os.PathSeparator)
	if abs, err := filepath.Abs(p); err != nil || !strings.HasPrefix(abs, filepath.Clean(ts.summaryDir())) || !strings.HasPrefix(abs, base) {
		return filepath.Join(userDir, "_invalid.json")
	}
	return p
}

func (ts *TeamixServer) loadSessionSummaries(user, sessionID string) []sessionSummary {
	data, err := os.ReadFile(ts.summaryFile(user, sessionID))
	if err != nil {
		return []sessionSummary{}
	}
	var out []sessionSummary
	if err := json.Unmarshal(data, &out); err != nil {
		return []sessionSummary{}
	}
	return out
}

func (ts *TeamixServer) saveSessionSummaries(user, sessionID string, sums []sessionSummary) {
	data, _ := json.MarshalIndent(sums, "", "  ")
	_ = os.WriteFile(ts.summaryFile(user, sessionID), data, 0o644)
}

// allSummaries 返回该用户所有会话的总结，按时间倒序，每条带来源会话
// （SessionID + 会话标题）。标题来自会话首条消息，缺失时为空串。
func (ts *TeamixServer) allSummaries(u *userSession) []sessionSummary {
	userDir := filepath.Join(ts.summaryDir(), u.name)
	entries, err := os.ReadDir(userDir)
	if err != nil {
		return []sessionSummary{}
	}
	sessionDir := u.ctrl.SessionDir()
	titleCache := map[string]string{}
	var out []sessionSummary
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".json")
		// 跳过路径防御产生的 _invalid 占位文件（不是真实会话）
		if sid == "_invalid" || sid == "" {
			continue
		}
		sums := ts.loadSessionSummaries(u.name, sid)
		if len(sums) == 0 {
			continue
		}
		title := titleCache[sid]
		if title == "" && sessionDir != "" {
			if first, _ := reasonixAgent.SessionPreview(filepath.Join(sessionDir, sid+".jsonl")); first != "" {
				title = sessionTitle(u.ctrl, sid+".jsonl", first)
				titleCache[sid] = title
			}
		}
		for i := range sums {
			sums[i].SessionID = sid
			sums[i].SessionTitle = title
			out = append(out, sums[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if out == nil {
		out = []sessionSummary{}
	}
	return out
}

// handleSummaries: GET 返回该用户所有会话的总结（每条标注来源会话）；
// POST 生成一条新的会话总结（AI 摘要，不改动会话本身）并追加到当前会话；
// DELETE 按 id 删除（跨会话文件扫描）。正在运行轮次时拒绝 POST，避免与
// provider 通道冲突。
func (ts *TeamixServer) handleSummaries(w http.ResponseWriter, r *http.Request, u *userSession) {
	sessionID := strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl")
	if sessionID == "" || sessionID == "." || sessionID == string(filepath.Separator) {
		http.Error(w, "no active session", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodPost {
		if u.ctrl.Running() {
			http.Error(w, "a turn is running; wait for it to finish", http.StatusConflict)
			return
		}
		var body struct {
			Instructions string `json:"instructions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		instructions := strings.TrimSpace(body.Instructions)
		if instructions == "" {
			instructions = "用自然语言总结对话内容，不要分门别类。"
		}
		content, err := u.ctrl.SessionSummary(r.Context(), instructions)
		if err != nil {
			http.Error(w, fmt.Sprintf("生成总结失败: %v", err), http.StatusBadGateway)
			return
		}
		title, rest := parseSummaryOutput(content)
		// load-modify-save 加锁，避免并发 POST 互相覆盖丢数据。
		ts.mu.Lock()
		sums := ts.loadSessionSummaries(u.name, sessionID)
		sums = append(sums, sessionSummary{
			ID:        fmt.Sprintf("s%d", time.Now().UnixNano()),
			Time:      time.Now(),
			Title:     title,
			Content:   rest,
			SessionID: sessionID,
		})
		ts.saveSessionSummaries(u.name, sessionID, sums)
		ts.mu.Unlock()
		writeJSON(w, ts.allSummaries(u))
		return
	}
	if r.Method == http.MethodDelete {
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ts.mu.Lock()
		userDir := filepath.Join(ts.summaryDir(), u.name)
		ents, _ := os.ReadDir(userDir)
		for _, e := range ents {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			sid := strings.TrimSuffix(e.Name(), ".json")
			sums := ts.loadSessionSummaries(u.name, sid)
			kept := sums[:0]
			changed := false
			for _, s := range sums {
				if s.ID == body.ID {
					changed = true
					continue
				}
				kept = append(kept, s)
			}
			if changed {
				ts.saveSessionSummaries(u.name, sid, kept)
			}
		}
		ts.mu.Unlock()
		writeJSON(w, ts.allSummaries(u))
		return
	}
	writeJSON(w, ts.allSummaries(u))
}

// parseSummaryOutput 解析 AI 按固定格式输出的总结：
//
//	<title>…</title>
//	<description>…</description>
//
// 只按标签取内容，不做任何字符串猜测/剥离；标签缺失（模型没按格式）时
// 整段当正文、不设标题，由前端用正文开头做回退标题。
var (
	summaryTitleRe = regexp.MustCompile(`(?s)<title>\s*(.*?)\s*</title>`)
	summaryDescRe  = regexp.MustCompile(`(?s)<description>\s*(.*?)\s*</description>`)
)

func parseSummaryOutput(content string) (title, rest string) {
	tm := summaryTitleRe.FindStringSubmatch(content)
	dm := summaryDescRe.FindStringSubmatch(content)
	if len(tm) == 2 && strings.TrimSpace(tm[1]) != "" {
		title = strings.TrimSpace(tm[1])
	}
	if len(dm) == 2 && strings.TrimSpace(dm[1]) != "" {
		rest = strings.TrimSpace(dm[1])
	}
	if title == "" && rest == "" {
		// 完全没有可用标签：整段当正文，不猜标题
		return "", strings.TrimSpace(content)
	}
	return title, rest
}
