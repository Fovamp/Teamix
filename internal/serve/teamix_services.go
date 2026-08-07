package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// runningService tracks a service process started by a user.
type runningService struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Service   string    `json:"service"` // 模块名
	Port      int       `json:"port"`    // 映射端口（用户输入）
	PID       int       `json:"pid"`
	Stage     string    `json:"stage"` // starting | running | failed
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	cmd       *exec.Cmd

	mu sync.Mutex // 保护 Stage/Error（进程退出 goroutine vs status 读）
}

// serviceManager tracks running services per user token.
type serviceManager struct {
	mu       sync.Mutex
	services map[string][]*runningService // token -> services
}

func newServiceManager() *serviceManager {
	return &serviceManager{services: make(map[string][]*runningService)}
}

func (sm *serviceManager) add(token string, svc *runningService) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.services[token] = append(sm.services[token], svc)
}

func (sm *serviceManager) list(token string) []*runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// 返回拷贝：调用方（sync 迭代对比）可能并发 remove，避免原地改底层 slice
	return append([]*runningService(nil), sm.services[token]...)
}

func (sm *serviceManager) find(token, id string) *runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.services[token] {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// findByModule 按 项目+模块 找运行中的服务（sync 对比用）。
func (sm *serviceManager) findByModule(token, project, module string) *runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.services[token] {
		if s.Project == project && s.Service == module {
			return s
		}
	}
	return nil
}

func (sm *serviceManager) remove(token, id string) *runningService {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	list := sm.services[token]
	for i, s := range list {
		if s.ID == id {
			sm.services[token] = append(list[:i], list[i+1:]...)
			return s
		}
	}
	return nil
}

func (sm *serviceManager) cleanupUser(token string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, s := range sm.services[token] {
		sm.killProcess(s)
	}
	delete(sm.services, token)
}

func (sm *serviceManager) killProcess(svc *runningService) {
	if svc.cmd == nil || svc.cmd.Process == nil {
		return
	}
	slog.Info("teamix: stopping service", "id", svc.ID, "pid", svc.PID)
	if runtime.GOOS == "windows" {
		// Windows 上 Signal(SIGTERM) 不可用，且只杀 cmd.exe 外壳会残留 java 子进程
		// 占端口 → taskkill /T 杀整棵进程树
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(svc.PID)).Run()
	} else {
		_ = svc.cmd.Process.Signal(syscall.SIGTERM)
		// 5s 兜底强杀（只发信号，不调 Wait——Wait 由 startService 退出 goroutine 负责）
		go func() {
			time.Sleep(5 * time.Second)
			_ = svc.cmd.Process.Kill()
		}()
	}
}

var svcMgr = newServiceManager()

// svcItem 是前端勾选的一个模块（项目 + 模块 + 映射端口）。
type svcItem struct {
	Project string `json:"project"`
	Module  string `json:"module"`
	Port    int    `json:"port"` // 映射端口（>0 必填）
}

// nacosEnv 构造启动子进程的 nacos 注入环境变量（per-process cmd.Env，不污染
// serve 全局环境——多用户各启各的 group，互不冲突）。nacos 未配置时返回空
// （测试项目可能无 nacos，直接不注入）。
func (ts *TeamixServer) nacosEnv(group string) []string {
	var out []string
	nc := ts.Nacos()
	if nc.ServerAddr == "" {
		return nil
	}
	ns := nc.Namespace
	if ns == "" {
		ns = "Teamix"
	}
	out = append(out,
		"SPRING_CLOUD_NACOS_CONFIG_NAMESPACE="+ns,
		"SPRING_CLOUD_NACOS_CONFIG_GROUP="+group,
		"SPRING_CLOUD_NACOS_DISCOVERY_NAMESPACE="+ns,
		"SPRING_CLOUD_NACOS_DISCOVERY_GROUP="+group,
		"SPRING_CLOUD_NACOS_CONFIG_SERVER_ADDR="+nc.ServerAddr,
		"SPRING_CLOUD_NACOS_DISCOVERY_SERVER_ADDR="+nc.ServerAddr,
	)
	if nc.Username != "" {
		out = append(out,
			"SPRING_CLOUD_NACOS_CONFIG_USERNAME="+nc.Username,
			"SPRING_CLOUD_NACOS_DISCOVERY_USERNAME="+nc.Username,
		)
	}
	if nc.Password != "" {
		out = append(out,
			"SPRING_CLOUD_NACOS_CONFIG_PASSWORD="+nc.Password,
			"SPRING_CLOUD_NACOS_DISCOVERY_PASSWORD="+nc.Password,
		)
	}
	return out
}

// portInUse 检查本机端口是否被占用（Listen 失败 = 占用）。
func portInUse(port int) bool {
	if port <= 0 {
		return false
	}
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	_ = l.Close()
	return false
}

// validatePorts 校验一组勾选模块的映射端口：
//   - 端口越界（<=0 或 >65535）→ port_invalid
//   - 本机端口被占用 → port_inuse
//   - 同用户多个模块映射同一端口 / 与已运行服务端口重复 → dup_port
// 冲突 key 用 "project/module"（跨项目同名模块不互串）。
func validatePorts(token string, items []svcItem) map[string]string {
	conflicts := map[string]string{}
	seen := map[int]string{} // port -> "project/module"
	for _, it := range items {
		key := it.Project + "/" + it.Module
		if it.Port <= 0 || it.Port > 65535 {
			conflicts[key] = "port_invalid"
			continue
		}
		if prev, dup := seen[it.Port]; dup {
			conflicts[key] = "dup_port:" + prev
			continue
		}
		seen[it.Port] = key
		if portInUse(it.Port) {
			conflicts[key] = "port_inuse"
			continue
		}
	}
	// 与当前用户已运行服务端口对比
	for _, s := range svcMgr.list(token) {
		if prev, ok := seen[s.Port]; ok && prev != s.Project+"/"+s.Service {
			conflicts[prev] = "dup_port:" + s.Project + "/" + s.Service
		}
	}
	return conflicts
}

// safeModuleName 模块名白名单：仅字母/数字/下划线/点/中划线（防 shell 注入）。
var safeModuleNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func safeModuleName(s string) bool {
	return s != "" && safeModuleNameRe.MatchString(s)
}

// startService 在用户目录启动一个模块（映射端口 + nacos 注入）。
// 返回 runningService（启动即登记，Stage=starting；进程退出转 failed 由 goroutine 处理）。
func (ts *TeamixServer) startService(u *userSession, projectName, module string, port int) (*runningService, error) {
	proj := ts.GlobalCfg().Projects.FindProject(projectName)
	if proj == nil {
		return nil, fmt.Errorf("project not found")
	}
	svc := proj.FindService(module)
	if svc == nil {
		return nil, fmt.Errorf("module %q not found in project", module)
	}
	projPath := filepath.Join(u.userRoot, projectName)
	svcPath := filepath.Join(projPath, filepath.FromSlash(svc.Dir))
	if _, err := os.Stat(svcPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project not cloned yet, select project first")
	}

	// 启动命令：mvn spring-boot:run -pl <模块>，映射端口用命令行参数覆盖（优先级最高）。
	// 模块名来自扫描器/配置（白名单内），再校验字符集防 shell 注入（&;|`$ 等）。
	if !safeModuleName(module) {
		return nil, fmt.Errorf("module name %q contains unsafe characters", module)
	}
	cmdLine := fmt.Sprintf("mvn spring-boot:run -pl %s -Dspring-boot.run.arguments=--server.port=%d", module, port)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdLine)
	} else {
		cmd = exec.Command("sh", "-c", cmdLine)
	}
	cmd.Dir = svcPath
	// per-process 环境：继承 serve 环境 + nacos 注入（group=用户名）
	env := append(os.Environ(), ts.nacosEnv(u.name)...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start failed: %w", err)
	}

	svcID := fmt.Sprintf("%s-%s-%d", projectName, module, cmd.Process.Pid)
	rs := &runningService{
		ID:        svcID,
		Project:   projectName,
		Service:   module,
		Port:      port,
		PID:       cmd.Process.Pid,
		Stage:     "starting",
		StartedAt: time.Now(),
		cmd:       cmd,
	}
	svcMgr.add(u.token, rs)
	slog.Info("teamix: service started", "user", u.name, "project", projectName,
		"module", module, "pid", cmd.Process.Pid, "port", port)

	// 进程退出：移除记录并标记失败（running 态由存活探测 goroutine 置）
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(done)
		rs.mu.Lock()
		if err != nil {
			rs.Stage = "failed"
			rs.Error = err.Error()
		}
		rs.mu.Unlock()
		slog.Info("teamix: service exited", "id", svcID, "err", err)
		svcMgr.remove(u.token, svcID)
	}()
	// 存活探测：启动 15s 后进程仍存活（未退出）→ 标记 running（编译/下载完成、服务已起）。
	// mvn 首次下载依赖可能超过 15s，此期间保持 starting，前端轮询持续展示。
	go func() {
		select {
		case <-time.After(15 * time.Second):
			rs.mu.Lock()
			if rs.Stage == "starting" {
				rs.Stage = "running"
			}
			rs.mu.Unlock()
		case <-done:
		}
	}()
	return rs, nil
}

// POST /teamix/services/validate {items} 校验映射端口（不启动）。
// 返回冲突表 {module: reason}；空表 = 全部可用。
func (ts *TeamixServer) handleServiceValidate(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Items []svcItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	conflicts := validatePorts(u.token, body.Items)
	writeJSON(w, map[string]any{"ok": true, "conflicts": conflicts})
}

// POST /teamix/projects/{name}/services/start 启动单个模块（兼容旧接口，
// body: {service, port?}）。新前端走 /teamix/services/sync。
func (ts *TeamixServer) handleServiceStart(w http.ResponseWriter, r *http.Request, u *userSession) {
	projectName := r.PathValue("name")
	var body struct {
		Service string `json:"service"`
		Port    int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Service == "" {
		http.Error(w, `{"error":"service name required"}`, http.StatusBadRequest)
		return
	}
	if body.Port <= 0 {
		http.Error(w, `{"error":"mapped port required"}`, http.StatusBadRequest)
		return
	}
	if c := validatePorts(u.token, []svcItem{{Project: projectName, Module: body.Service, Port: body.Port}}); len(c) > 0 {
		http.Error(w, `{"error":"port conflict: `+c[projectName+"/"+body.Service]+`"}`, http.StatusConflict)
		return
	}
	rs, err := ts.startService(u, projectName, body.Service, body.Port)
	if err != nil {
		http.Error(w, `{"error":"`+jsonEscape(err.Error())+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": rs.ID, "pid": rs.PID, "port": rs.Port, "service": body.Service})
}

// POST /teamix/services/sync {items} 同步勾选集合：
// 勾了没跑 → 启动；勾了在跑（端口相同）→ 不动；勾了在跑（端口变了）→ 重启；
// 没勾在跑 → 关闭。返回每项结果。
func (ts *TeamixServer) handleServiceSync(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		Items []svcItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// 先校验端口（冲突则整体拒绝，前端标红重试）
	conflicts := validatePorts(u.token, body.Items)
	if len(conflicts) > 0 {
		writeJSON(w, map[string]any{"ok": false, "conflicts": conflicts})
		return
	}

	type result struct {
		Project string `json:"project"`
		Module  string `json:"module"`
		Port    int    `json:"port"`
		Action  string `json:"action"` // started | running | restarted | stopped | skipped
		Error   string `json:"error,omitempty"`
	}
	var results []result

	want := map[string]svcItem{} // "proj/module" -> item
	for _, it := range body.Items {
		if it.Module == "" {
			continue
		}
		want[it.Project+"/"+it.Module] = it
	}

	// 1) 关闭：运行中但不在勾选集合
	for _, s := range svcMgr.list(u.token) {
		if _, ok := want[s.Project+"/"+s.Service]; !ok {
			svcMgr.killProcess(s)
			svcMgr.remove(u.token, s.ID)
			results = append(results, result{Project: s.Project, Module: s.Service, Port: s.Port, Action: "stopped"})
		}
	}

	// 2) 启动/不动/重启：勾选集合里的
	for _, it := range want {
		existing := svcMgr.findByModule(u.token, it.Project, it.Module)
		if existing == nil {
			rs, err := ts.startService(u, it.Project, it.Module, it.Port)
			if err != nil {
				results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "failed", Error: err.Error()})
			} else {
				results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "started"})
				_ = rs
			}
			continue
		}
		if existing.Port == it.Port {
			// 同端口：运行中 → 不动；启动中（下载依赖/编译）→ 不动（避免二次 sync 误杀）；
			// 已失败或刚退出（goroutine 未 remove）→ 重启
			existing.mu.Lock()
			st := existing.Stage
			existing.mu.Unlock()
			if st == "running" {
				results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "running"})
				continue
			}
			if st == "starting" {
				results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "running"})
				continue
			}
		}
		// 端口变了 → 重启
		svcMgr.killProcess(existing)
		svcMgr.remove(u.token, existing.ID)
		rs, err := ts.startService(u, it.Project, it.Module, it.Port)
		if err != nil {
			results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "failed", Error: err.Error()})
		} else {
			results = append(results, result{Project: it.Project, Module: it.Module, Port: it.Port, Action: "restarted"})
			_ = rs
		}
	}

	writeJSON(w, map[string]any{"ok": true, "results": results})
}

func (ts *TeamixServer) handleServiceStop(w http.ResponseWriter, r *http.Request, u *userSession) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"service id required"}`, http.StatusBadRequest)
		return
	}

	svc := svcMgr.find(u.token, body.ID)
	if svc == nil {
		http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
		return
	}

	svcMgr.killProcess(svc)
	svcMgr.remove(u.token, body.ID)

	writeJSON(w, map[string]bool{"ok": true})
}

func (ts *TeamixServer) handleServicesStatus(w http.ResponseWriter, r *http.Request, u *userSession) {
	list := svcMgr.list(u.token)
	if list == nil {
		list = []*runningService{}
	}
	type svcStatus struct {
		ID        string `json:"id"`
		Project   string `json:"project"`
		Service   string `json:"service"`
		Port      int    `json:"port"`
		PID       int    `json:"pid"`
		Stage     string `json:"stage"`
		Error     string `json:"error,omitempty"`
		StartedAt string `json:"startedAt"`
	}
	out := make([]svcStatus, len(list))
	for i, s := range list {
		s.mu.Lock()
		out[i] = svcStatus{
			ID: s.ID, Project: s.Project, Service: s.Service,
			Port: s.Port, PID: s.PID, Stage: s.Stage, Error: s.Error,
			StartedAt: s.StartedAt.Format(time.RFC3339),
		}
		s.mu.Unlock()
	}
	writeJSON(w, out)
}
