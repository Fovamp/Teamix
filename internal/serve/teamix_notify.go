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

type notification struct {
	ID          string    `json:"id"`
	FromUser    string    `json:"fromUser"`
	ToUser      string    `json:"toUser"`
	Project     string    `json:"project,omitempty"`
	Message     string    `json:"message"`
	FileChanged string    `json:"fileChanged,omitempty"`
	Read        bool      `json:"read"`
	Time        time.Time `json:"time"`
}


// Notification handlers.

func (ts *TeamixServer) notiDir() string {
	base := ts.workspaceRoot
	if base == "" {
		base = config.ReasonixHomeDir()
	}
	dir := filepath.Join(base, ".teamix", "notifications")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}


func (ts *TeamixServer) notiFile(user string) string {
	return filepath.Join(ts.notiDir(), user+".json")
}

// notiFileProject returns path for user+project notifications.
func (ts *TeamixServer) notiFileProject(user, project string) string {
	if project == "" {
		return ts.notiFile(user)
	}
	userDir := filepath.Join(ts.notiDir(), user)
	_ = os.MkdirAll(userDir, 0o755)
	p := filepath.Join(userDir, project+".json")
	// 防御：确保结果始终位于 notiDir 之内，防止 user/project 携带 .. 逃逸。
	base := filepath.Clean(ts.notiDir()) + string(os.PathSeparator)
	if abs, err := filepath.Abs(p); err != nil || !strings.HasPrefix(abs, filepath.Clean(ts.notiDir())) || !strings.HasPrefix(abs, base) {
		return filepath.Join(userDir, "_invalid.json")
	}
	return p
}

// safeTokenName 校验标识符（用户名/项目名/MCP 名）只含安全字符，防止路径穿越与 TOML 注入。
func safeTokenName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}


func (ts *TeamixServer) loadNotifications(user string) []notification {
	data, err := os.ReadFile(ts.notiFile(user))
	if err != nil {
		return []notification{}
	}
	var notis []notification
	if err := json.Unmarshal(data, &notis); err != nil {
		return []notification{}
	}
	for i := range notis {
		if notis[i].ID == "" {
			notis[i].ID = fmt.Sprintf("n%d", i)
		}
	}
	return notis
}


func (ts *TeamixServer) loadNotificationsProject(user, project string) []notification {
	data, err := os.ReadFile(ts.notiFileProject(user, project))
	if err != nil {
		return []notification{}
	}
	var notis []notification
	if err := json.Unmarshal(data, &notis); err != nil {
		return []notification{}
	}
	for i := range notis {
		if notis[i].ID == "" {
			notis[i].ID = fmt.Sprintf("n%d", i)
		}
	}
	return notis
}

func (ts *TeamixServer) saveNotifications(user string, notis []notification) {
	data, _ := json.MarshalIndent(notis, "", "  ")
	_ = os.WriteFile(ts.notiFile(user), data, 0o644)
}

func (ts *TeamixServer) saveNotificationsProject(user, project string, notis []notification) {
	data, _ := json.MarshalIndent(notis, "", "  ")
	_ = os.WriteFile(ts.notiFileProject(user, project), data, 0o644)
}


func (ts *TeamixServer) handleNotifications(w http.ResponseWriter, r *http.Request, u *userSession) {
	project := r.URL.Query().Get("project")
	if project != "" {
		notis := ts.loadNotificationsProject(u.name, project)
		writeJSON(w, notis)
		return
	}
	notis := ts.loadNotifications(u.name)
	writeJSON(w, notis)
}


func (ts *TeamixServer) handleNotificationRead(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID      string `json:"id"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Project != "" && !safeTokenName(body.Project) {
		http.Error(w, "invalid project name", http.StatusBadRequest)
		return
	}
	markRead := func(notis []notification) bool {
		changed := false
		for i := range notis {
			if body.ID == "" || notis[i].ID == body.ID {
				if !notis[i].Read {
					notis[i].Read = true
					changed = true
				}
				if body.ID != "" {
					break
				}
			}
		}
		return changed
	}
	if body.Project != "" {
		notis := ts.loadNotificationsProject(u.name, body.Project)
		if markRead(notis) {
			ts.saveNotificationsProject(u.name, body.Project, notis)
		}
	} else {
		notis := ts.loadNotifications(u.name)
		if markRead(notis) {
			ts.saveNotifications(u.name, notis)
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}


func (ts *TeamixServer) handleNotificationCreate(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ToUser      string `json:"toUser"`
		Project     string `json:"project,omitempty"`
		Message     string `json:"message"`
		FileChanged string `json:"fileChanged,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ToUser == "" || body.Message == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !safeTokenName(body.ToUser) || (body.Project != "" && !safeTokenName(body.Project)) {
		http.Error(w, "invalid user or project name", http.StatusBadRequest)
		return
	}
	noti := notification{
		ID:          fmt.Sprintf("n%d", time.Now().UnixNano()),
		FromUser:    u.name,
		ToUser:      body.ToUser,
		Project:     body.Project,
		Message:     body.Message,
		FileChanged: body.FileChanged,
		Read:        false,
		Time:        time.Now(),
	}
	appendNoti := func(list []notification) []notification {
		if len(list) > 100 {
			list = list[len(list)-100:]
		}
		return append(list, noti)
	}
	if body.Project != "" {
		ts.saveNotificationsProject(body.ToUser, body.Project, appendNoti(ts.loadNotificationsProject(body.ToUser, body.Project)))
	} else {
		ts.saveNotifications(body.ToUser, appendNoti(ts.loadNotifications(body.ToUser)))
	}
	writeJSON(w, map[string]bool{"ok": true})
}

