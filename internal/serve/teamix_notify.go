package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	return filepath.Join(userDir, project+".json")
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	notis := ts.loadNotifications(u.name)
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
	if changed {
		ts.saveNotifications(u.name, notis)
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
	notis := ts.loadNotifications(body.ToUser)
	if len(notis) > 100 {
		notis = notis[len(notis)-100:]
	}
	notis = append(notis, noti)
	ts.saveNotifications(body.ToUser, notis)
	writeJSON(w, map[string]bool{"ok": true})
}

