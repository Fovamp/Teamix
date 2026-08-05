package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
)

// sessionSummary 是"会话总结"面板里的一条记录：AI 生成的、面向人阅读的
// 当前会话摘要，展示在左侧栏"总结"面板，按会话（sessionID）隔离存储。
// 它与 /summarize（SummarizeUpTo/From，会改写会话消息日志的压缩操作）不同：
// 这里生成后完全不改动会话本身。
type sessionSummary struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Content string    `json:"content"`
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

// handleSummaries: GET 返回当前会话的总结列表；POST 生成一条新的会话总结
// （AI 摘要，不改动会话本身）并追加保存。正在运行轮次时拒绝，避免与
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
			instructions = "面向用户阅读的会话总结：概括主要目标、已完成的事、关键结论与尚未完成的事项。"
		}
		content, err := u.ctrl.SessionSummary(r.Context(), instructions)
		if err != nil {
			http.Error(w, fmt.Sprintf("生成总结失败: %v", err), http.StatusBadGateway)
			return
		}
		// load-modify-save 加锁，避免并发 POST 互相覆盖丢数据。
		ts.mu.Lock()
		sums := ts.loadSessionSummaries(u.name, sessionID)
		sums = append(sums, sessionSummary{
			ID:      fmt.Sprintf("s%d", time.Now().UnixNano()),
			Time:    time.Now(),
			Content: content,
		})
		ts.saveSessionSummaries(u.name, sessionID, sums)
		ts.mu.Unlock()
		writeJSON(w, sums)
		return
	}
	writeJSON(w, ts.loadSessionSummaries(u.name, sessionID))
}
