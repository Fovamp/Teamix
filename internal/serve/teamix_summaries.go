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
)

// sessionSummary 是"会话总结"面板里的一条记录：AI 生成的、面向人阅读的
// 会话摘要。与 /summarize（SummarizeUpTo/From，会改写会话消息日志的压缩操作）
// 不同：这里生成后完全不改动会话本身。Title 为简短标题；Description 为一两句
// 概述（列表/悬浮展示）；Content 为完整正文（展开查看）；
// SessionID/SessionTitle 标注这条总结来自哪个会话；Project 标注来自哪个项目。
type sessionSummary struct {
	ID           string    `json:"id"`
	Time         time.Time `json:"time"`
	Title        string    `json:"title,omitempty"`
	Description  string    `json:"description,omitempty"`
	Content      string    `json:"content"`
	SessionID    string    `json:"sessionId,omitempty"`
	SessionTitle string    `json:"sessionTitle,omitempty"`
	Project      string    `json:"project,omitempty"`
}

// summaryDir 返回会话总结的存储根目录（用户私有：<userRoot>/.teamix/<project>/summaries/）。
// 目录结构：summaries/<sessionID>.json（项目内平铺）。总结是用户私人数据，
// 放用户目录且跟随项目（与 memory/sessions 一致：<project>/ 下），项目克隆保持纯净。
func (ts *TeamixServer) summaryDir(userRoot, project string) string {
	dir := filepath.Join(userRoot, ".teamix", project, "summaries")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// summaryFile 返回某用户某项目下某会话的总结文件。sessionID 来自 SessionPath
// 的文件名；project 为空时归入 _legacy（历史数据）。路径穿越防御：
// 统一转绝对路径后再比较。
func (ts *TeamixServer) summaryFile(userRoot, project, sessionID string) string {
	dir := ts.summaryDir(userRoot, project)
	p := filepath.Join(dir, sessionID+".json")
	absBase, absErr := filepath.Abs(filepath.Clean(dir))
	absP, err := filepath.Abs(p)
	if err != nil || absErr != nil || !strings.HasPrefix(absP, absBase+string(os.PathSeparator)) {
		return filepath.Join(dir, "_invalid.json")
	}
	return p
}

func (ts *TeamixServer) loadSessionSummaries(userRoot, project, sessionID string) []sessionSummary {
	data, err := os.ReadFile(ts.summaryFile(userRoot, project, sessionID))
	if err != nil {
		return []sessionSummary{}
	}
	var out []sessionSummary
	if err := json.Unmarshal(data, &out); err != nil {
		return []sessionSummary{}
	}
	return out
}

func (ts *TeamixServer) saveSessionSummaries(userRoot, project, sessionID string, sums []sessionSummary) error {
	data, err := json.MarshalIndent(sums, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ts.summaryFile(userRoot, project, sessionID), data, 0o644)
}

// allSummaries 返回当前选中项目下该用户所有会话的总结，按时间倒序，
// 每条带来源会话与项目。未选择项目时返回空（前端提示先选项目——
// 总结按项目隔离，点击对应项目后才读取）。
func (ts *TeamixServer) allSummaries(u *userSession) []sessionSummary {
	project := u.selectedProject
	if project == "" {
		return []sessionSummary{}
	}
	projDir := ts.summaryDir(u.userRoot, project)
	entries, err := os.ReadDir(projDir)
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
		sums := ts.loadSessionSummaries(u.userRoot, project, sid)
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
			sums[i].Project = project
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
// provider 通道冲突。GET 不要求当前会话存在（查看历史总结不依赖活动会话）。
func (ts *TeamixServer) handleSummaries(w http.ResponseWriter, r *http.Request, u *userSession) {
	sessionID := strings.TrimSuffix(filepath.Base(u.ctrl.SessionPath()), ".jsonl")
	if r.Method == http.MethodPost {
		if sessionID == "" || sessionID == "." || sessionID == string(filepath.Separator) {
			http.Error(w, "no active session", http.StatusBadRequest)
			return
		}
		// 总结按项目隔离：未选择项目不允许生成/读取
		if u.selectedProject == "" {
			http.Error(w, "请先选择项目再生成总结", http.StatusBadRequest)
			return
		}
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
		title, desc, content := parseSummaryOutput(content)
		// load-modify-save 加锁，避免并发 POST 互相覆盖丢数据。
		ts.mu.Lock()
		sums := ts.loadSessionSummaries(u.userRoot, u.selectedProject, sessionID)
		sums = append(sums, sessionSummary{
			ID:          fmt.Sprintf("s%d", time.Now().UnixNano()),
			Time:        time.Now(),
			Title:       title,
			Description: desc,
			Content:     content,
			SessionID:   sessionID,
			Project:     u.selectedProject,
		})
		if err := ts.saveSessionSummaries(u.userRoot, u.selectedProject, sessionID, sums); err != nil {
			ts.mu.Unlock()
			http.Error(w, fmt.Sprintf("保存总结失败: %v", err), http.StatusInternalServerError)
			return
		}
		ts.mu.Unlock()
		writeJSON(w, ts.allSummaries(u))
		return
	}
	if r.Method == http.MethodDelete {
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if u.selectedProject == "" {
			http.Error(w, "请先选择项目", http.StatusBadRequest)
			return
		}
		ts.mu.Lock()
		projDir := ts.summaryDir(u.userRoot, u.selectedProject)
		ents, _ := os.ReadDir(projDir)
		for _, e := range ents {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			sid := strings.TrimSuffix(e.Name(), ".json")
			sums := ts.loadSessionSummaries(u.userRoot, u.selectedProject, sid)
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
				_ = ts.saveSessionSummaries(u.userRoot, u.selectedProject, sid, kept)
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
//	<content>…</content>
//
// 只按标签取内容，不做任何字符串猜测/剥离；标签缺失（模型没按格式）时
// 整段当正文、不设标题与描述，由前端用正文开头做回退标题。
var (
	summaryTitleRe = regexp.MustCompile(`(?s)<title>\s*(.*?)\s*</title>`)
	summaryDescRe  = regexp.MustCompile(`(?s)<description>\s*(.*?)\s*</description>`)
	summaryBodyRe  = regexp.MustCompile(`(?s)<content>\s*(.*?)\s*</content>`)
)

func parseSummaryOutput(content string) (title, description, body string) {
	tm := summaryTitleRe.FindStringSubmatch(content)
	dm := summaryDescRe.FindStringSubmatch(content)
	bm := summaryBodyRe.FindStringSubmatch(content)
	if len(tm) == 2 && strings.TrimSpace(tm[1]) != "" {
		title = strings.TrimSpace(tm[1])
	}
	if len(dm) == 2 && strings.TrimSpace(dm[1]) != "" {
		description = strings.TrimSpace(dm[1])
	}
	if len(bm) == 2 && strings.TrimSpace(bm[1]) != "" {
		body = strings.TrimSpace(bm[1])
	}
	if title == "" && description == "" && body == "" {
		// 完全没有可用标签：整段当正文，不猜标题
		return "", "", strings.TrimSpace(content)
	}
	// 有 description 但缺 content 时正文回退到 description
	if body == "" && description != "" {
		body = description
	}
	return title, description, body
}
