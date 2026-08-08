package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// limitedBuffer 滚动保留尾部内容的 writer（供服务输出捕获，防止无限增长）。
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func newLimitedBuffer(max int) *limitedBuffer {
	return &limitedBuffer{max: max}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len()+len(p) > b.max {
		excess := b.buf.Len() + len(p) - b.max
		if excess >= b.buf.Len() {
			b.buf.Reset()
		} else {
			_ = b.buf.Next(excess)
		}
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runningService tracks a service process started by a user.
type runningService struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Service   string    `json:"service"` // 模块名
	Port      int       `json:"port"`    // 映射端口（用户输入）
	PID       int       `json:"pid"`
	Stage     string    `json:"stage"` // starting | running | failed | stopped
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	cmd       *exec.Cmd
	out       *limitedBuffer // 进程输出（滚动保留，status 返回供查看）
	logPath   string         // 完整日志文件（users/<user>/.teamix/logs/services/，进程退出后仍可读）

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

// safeModPath 模块相对路径白名单（-pl 参数）：路径字符 + / 分隔。
var safeModPathRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func safeModPath(s string) bool {
	return s != "" && safeModPathRe.MatchString(s)
}

// lookPathMaven 定位 Maven 可执行文件（返回绝对路径，启动命令直接用）：
//  1. 进程 PATH（mvn / mvn.cmd / mvn.bat / mvn.exe）
//  2. 进程环境 MAVEN_HOME / MVN_HOME
//  3. Windows 用户注册表（HKCU\Environment）的 MAVEN_HOME / MVN_HOME / Path
//     ——不依赖进程环境继承（从旧终端启动 teamix.exe 也能读到）
//  4. 常见安装位置兜底（~/Documents/apache-maven-*、C:\apache-maven-*）
func lookPathMaven() (string, error) {
	if p, err := exec.LookPath("mvn"); err == nil {
		return p, nil
	}
	for _, name := range []string{"mvn.cmd", "mvn.bat", "mvn.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, env := range []string{"MAVEN_HOME", "MVN_HOME"} {
		if p := mavenInHome(os.Getenv(env)); p != "" {
			return p, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"MAVEN_HOME", "MVN_HOME"} {
			if p := mavenInHome(readUserEnv(env)); p != "" {
				return p, nil
			}
		}
		// 注册表用户 PATH 逐段查找 mvn
		if up := readUserEnv("Path"); up != "" {
			for _, seg := range filepath.SplitList(up) {
				for _, name := range []string{"mvn.cmd", "mvn.bat", "mvn.exe"} {
					p := filepath.Join(seg, name)
					if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
						return p, nil
					}
				}
			}
		}
	}
	// 常见安装位置兜底
	for _, base := range []string{
		filepath.Join(os.Getenv("USERPROFILE"), "Documents"),
		os.Getenv("USERPROFILE"),
		"C:\\",
	} {
		if base == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(base, "apache-maven-*", "bin", "mvn.cmd"))
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("mvn 不在 PATH，也未设置 MAVEN_HOME")
}

// findReactorRoot 从模块目录向上找最近的聚合 pom（含 <modules>）作为 Maven
// reactor 根——嵌套仓库（如 JeecgBoot 的 jeecg-boot/ 子目录）克隆根不是 reactor 根，
// -pl 相对路径/artifactId 都可能找不到。返回 reactor 根目录 + 模块相对路径。
func findReactorRoot(svcPath string) (root, relPath string) {
	dir := filepath.Clean(svcPath)
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "pom.xml")); err == nil && bytes.Contains(data, []byte("<modules>")) {
			rel, _ := filepath.Rel(dir, svcPath)
			return dir, filepath.ToSlash(rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

// lookPathPnpm 定位 pnpm（PATH 中 pnpm / pnpm.cmd / pnpm.exe）。
func lookPathPnpm() (string, error) {
	if p, err := exec.LookPath("pnpm"); err == nil {
		return p, nil
	}
	for _, name := range []string{"pnpm.cmd", "pnpm.bat", "pnpm.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("pnpm 不在 PATH")
}

// newCmdScript 生成并返回一个执行脚本的 cmd（Windows 专用）：
// 脚本写入 users/<user>/.teamix/tmp/，绝对路径，绕开 cmd /c 的引号解析问题。
func newCmdScript(u *userSession, projectName, module string, port int, script, kind string) *exec.Cmd {
	scriptPath := filepath.Join(u.userRoot, ".teamix", "tmp",
		fmt.Sprintf("%s-%s-%s-%d.cmd", kind, projectName, module, port))
	if abs, err := filepath.Abs(scriptPath); err == nil {
		scriptPath = abs
	}
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err == nil {
		_ = os.WriteFile(scriptPath, []byte(script), 0o644)
	}
	return exec.Command("cmd", "/c", scriptPath)
}

// mavenInHome 检查 MAVEN_HOME/MVN_HOME 目录下的 mvn 可执行文件。
func mavenInHome(home string) string {
	if home == "" {
		return ""
	}
	for _, name := range []string{"bin/mvn.cmd", "bin/mvn.bat", "bin/mvn"} {
		p := filepath.Join(home, filepath.FromSlash(name))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// syncFrontendProxyEnv 让前端 vite proxy 自动指向用户映射的后端端口：
// 读取前端模块 .env.development（缺失则 .env），把其中所有 VITE_* 行里的
// localhost:<后端原端口> 替换为 localhost:<映射端口>（仅替换正在运行/启动中的
// backend 模块），写入 gitignored 的 .env.development.local——vite 加载优先级
// 高于 .env.development，覆盖生效且不污染项目跟踪文件。
// 无运行中后端 / 无 VITE_* 引用 / 文件不可写 → 静默跳过，不影响启动。
func (ts *TeamixServer) syncFrontendProxyEnv(u *userSession, projectName, svcPath string) {
	proj := ts.GlobalCfg().Projects.FindProject(projectName)
	if proj == nil {
		return
	}
	// 收集正在运行/启动中的 backend 模块：原端口 -> 映射端口
	portMap := map[int]int{}
	for _, rs := range svcMgr.list(u.token) {
		if rs.Project != projectName || rs.Port <= 0 || rs.Stage == "stopped" || rs.Stage == "failed" {
			continue
		}
		svc := proj.FindService(rs.Service)
		if svc == nil || svc.Type != "backend" || svc.Port <= 0 || svc.Port == rs.Port {
			continue
		}
		portMap[svc.Port] = rs.Port
	}
	if len(portMap) == 0 {
		return
	}
	// 读取 .env.development（优先）/ .env
	src := filepath.Join(svcPath, ".env.development")
	if _, err := os.Stat(src); err != nil {
		src = filepath.Join(svcPath, ".env")
		if _, err := os.Stat(src); err != nil {
			return
		}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimPrefix(string(data), "\xef\xbb\xbf"), "\n")
	var out []string
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 只处理 VITE_* 且值里含 localhost: 的行（proxy target / api url 等）
		if !strings.HasPrefix(trimmed, "VITE_") || !strings.Contains(trimmed, "localhost:") {
			continue
		}
		newLine := line
		for orig, mapped := range portMap {
			newLine = strings.ReplaceAll(newLine, fmt.Sprintf("localhost:%d", orig), fmt.Sprintf("localhost:%d", mapped))
		}
		if newLine != line {
			changed = true
			out = append(out, newLine)
		}
	}
	if !changed {
		return
	}
	// 合并已有 .env.development.local：移除旧的 VITE_* 行（避免重复/陈旧端口），
	// 保留用户其他手动内容，再追加本次生成的替换行。
	local := filepath.Join(svcPath, ".env.development.local")
	var keep []string
	if old, err := os.ReadFile(local); err == nil {
		for _, l := range strings.Split(strings.TrimPrefix(string(old), "\xef\xbb\xbf"), "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "VITE_") && strings.Contains(t, "localhost:") {
				continue
			}
			keep = append(keep, l)
		}
	}
	content := strings.Join(append(keep, out...), "\n")
	_ = os.WriteFile(local, []byte(content), 0o644)
}

// startService 在用户目录启动一个模块（映射端口 + nacos 注入）。
// 返回 runningService（启动即登记，Stage=starting；进程退出转 failed 由 goroutine 处理）。
// 任何失败也会登记一条 failed 记录（抽屉可见、可重试），而不是静默消失。
func (ts *TeamixServer) startService(u *userSession, projectName, module string, port int) (*runningService, error) {
	// recordFail：清理同模块旧记录并登记 failed（抽屉可见失败原因 + 可重试）
	recordFail := func(err error) (*runningService, error) {
		for _, old := range svcMgr.list(u.token) {
			if old.Project == projectName && old.Service == module {
				svcMgr.remove(u.token, old.ID)
			}
		}
		rs := &runningService{
			ID:        fmt.Sprintf("%s-%s-fail", projectName, module),
			Project:   projectName,
			Service:   module,
			Port:      port,
			Stage:     "failed",
			Error:     err.Error(),
			StartedAt: time.Now(),
		}
		svcMgr.add(u.token, rs)
		return nil, err
	}

	proj := ts.GlobalCfg().Projects.FindProject(projectName)
	if proj == nil {
		return recordFail(fmt.Errorf("project not found"))
	}
	svc := proj.FindService(module)
	if svc == nil {
		return recordFail(fmt.Errorf("module %q not found in project", module))
	}
	projPath := filepath.Join(u.userRoot, projectName)
	svcPath := filepath.Join(projPath, filepath.FromSlash(svc.Dir))
	// 绝对化：userRoot 可能是相对路径，cmd.Dir 改变后相对 scriptPath 会在模块目录下
	// 解析 → "The system cannot find the path specified."
	if abs, err := filepath.Abs(svcPath); err == nil {
		svcPath = abs
	}
	if _, err := os.Stat(svcPath); os.IsNotExist(err) {
		return recordFail(fmt.Errorf("project not cloned yet, select project first"))
	}

	// 执行目录：前端 = 模块目录（svcPath，package.json 所在处）；
	// 后端 = 项目根（projPath，聚合 pom，-pl 指定 reactor 模块）。
	if !safeModuleName(module) {
		return recordFail(fmt.Errorf("module name %q contains unsafe characters", module))
	}
	var cmd *exec.Cmd
	if svc.Type == "frontend" {
		// 前端模块：pnpm 下载依赖 + vite 启动（dev script 透传 --port 覆盖映射端口）
		// 先同步 vite proxy 到映射端口（.env.development.local，见 syncFrontendProxyEnv）
		ts.syncFrontendProxyEnv(u, projectName, svcPath)
		if _, err := lookPathPnpm(); err != nil {
			return recordFail(fmt.Errorf("未检测到 pnpm：%v（请安装 pnpm 并加入 PATH 后重试）", err))
		}
		if runtime.GOOS == "windows" {
			// pnpm 11 移除了 onlyBuiltDependencies 配置（--config.onlyBuiltDependencies[]=*
			// 已失效，install 会因 strictDepBuilds 报 ERR_PNPM_IGNORED_BUILDS 退出 1）。
			// 改用 --config.dangerouslyAllowAllBuilds=true（等效 approve-builds 全选，
			// esbuild/less 的 postinstall 正常执行，vite 运行时二进制就绪）。
			// dev 前用 --config.verifyDepsBeforeRun=false 关掉 pnpm run 的依赖状态检查
			// （默认 install：node_modules 状态不符会再自动裸跑一次 install，同样
			// 因 ignored builds 失败，报错堆栈即 runDepsStatusCheck）。
			// 注意：--config.* 必须放在 pnpm 命令名之前才会被 pnpm 消费，
			// 放在 dev 之后会被透传给 vite 导致启动失败。
			// install 输出重定向到临时文件（成功即删、失败才 type 出来），
			// 避免占用 32KB 滚动缓冲把 dev 阶段的真实报错挤掉。
			script := fmt.Sprintf("@echo off\r\nchcp 65001>nul\r\nif not exist node_modules (\r\n  pnpm --config.dangerouslyAllowAllBuilds=true install >\"%%TEMP%%\\teamix-%s-pnpm-install.log\" 2>&1\r\n  if errorlevel 1 (\r\n    echo [install failed] last output:\r\n    type \"%%TEMP%%\\teamix-%s-pnpm-install.log\"\r\n    del \"%%TEMP%%\\teamix-%s-pnpm-install.log\" >nul 2>&1\r\n    exit /b 1\r\n  )\r\n  del \"%%TEMP%%\\teamix-%s-pnpm-install.log\" >nul 2>&1\r\n)\r\ncall pnpm --config.verifyDepsBeforeRun=false dev --port %d\r\n", module, module, module, module, port)
			cmd = newCmdScript(u, projectName, module, port, script, "pnpm")
		} else {
			cmdLine := fmt.Sprintf("if [ ! -d node_modules ]; then IL=$(mktemp); pnpm --config.dangerouslyAllowAllBuilds=true install >\"$IL\" 2>&1 && rm -f \"$IL\" || { cat \"$IL\"; rm -f \"$IL\"; exit 1; }; fi; pnpm --config.verifyDepsBeforeRun=false dev --port %d", port)
			cmd = exec.Command("sh", "-c", cmdLine)
		}
		cmd.Dir = svcPath
	} else {
		// 后端模块：Maven reactor 构建。从模块目录向上找聚合 pom（reactor 根），
		// 在根执行 `mvn spring-boot:run -pl <相对路径> -am`（-am 连带构建兄弟模块）。
		mvnPath, err := lookPathMaven()
		if err != nil {
			return recordFail(fmt.Errorf("未检测到 Maven：%v（请安装 Maven 并加入 PATH，或配置 MAVEN_HOME 后重试）", err))
		}
		reactorRoot, relPath := findReactorRoot(svcPath)
		runDir := projPath // 兜底：克隆根
		plArg := ":" + module
		if reactorRoot != "" && relPath != "" {
			runDir = reactorRoot
			plArg = relPath
		}
		if !safeModPath(plArg) {
			return recordFail(fmt.Errorf("module path %q contains unsafe characters", plArg))
		}
		// 两步启动：
		//  1) mvn install -DskipTests -pl <X> -am —— 构建并安装兄弟模块到 .m2（不运行）
		//  2) mvn spring-boot:run -pl <X> —— 单模块运行（兄弟依赖从 .m2 解析）
		// 注意：不能把 spring-boot:run 与 -am 同用——-am 会让依赖模块也执行 run，
		// 聚合 pom 模块（无 main class）会 "Unable to find a suitable main class"。
		if runtime.GOOS == "windows" {
			// 临时 .cmd 脚本绕开 cmd 引号解析问题。
			// 注意：不能用 -llr（Maven 3.9.1+ 已移除该选项，会直接报错）。
			// install 输出重定向到 %TEMP% 临时文件（成功即删、失败才 type 出来），
			// 避免 30s 编译日志占满 32KB 滚动缓冲、把 spring-boot:run 的真实报错
			// （如端口占用/启动异常）挤掉——这是此前"进程已退出"难排查的根因。
			// 必须用 call 调用 mvn.cmd：cmd 批处理里直接调另一个 .cmd 时，内层
			// 结束后外层脚本即终止（控制权不返回），spring-boot:run 永远不会执行
			// （症状：日志 0 字节 + 进程已退出，实测 2026-08-08）。
			script := fmt.Sprintf("@echo off\r\nchcp 65001>nul\r\nset JAVA_TOOL_OPTIONS=-Dfile.encoding=UTF-8\r\nset IL=%%TEMP%%\\teamix-%s-install.log\r\ncall \"%s\" install -DskipTests -pl %s -am >\"%%IL%%\" 2>&1\r\nif errorlevel 1 (\r\n  echo [install failed] last output:\r\n  type \"%%IL%%\"\r\n  del \"%%IL%%\" >nul 2>&1\r\n  exit /b 1\r\n)\r\ndel \"%%IL%%\" >nul 2>&1\r\ncall \"%s\" spring-boot:run -pl %s -Dspring-boot.run.arguments=--server.port=%d\r\n",
				module, mvnPath, plArg, mvnPath, plArg, port)
			cmd = newCmdScript(u, projectName, module, port, script, "mvn")
		} else {
			cmdLine := fmt.Sprintf("IL=$(mktemp); %q install -DskipTests -pl %s -am >\"$IL\" 2>&1 && rm -f \"$IL\" || { cat \"$IL\"; rm -f \"$IL\"; exit 1; }; %q spring-boot:run -pl %s -Dspring-boot.run.arguments=--server.port=%d",
				mvnPath, plArg, mvnPath, plArg, port)
			cmd = exec.Command("sh", "-c", cmdLine)
		}
		cmd.Dir = runDir
	}
	// per-process 环境：继承 serve 环境 + nacos 注入（group=用户名）
	env := append(os.Environ(), ts.nacosEnv(u.name)...)
	cmd.Env = env
	// 日志落盘：users/<user>/.teamix/logs/services/<project>-<module>-<port>.log，
	// 每次启动覆盖；run 阶段输出完整落盘（install 输出已隔离到临时文件，失败才可见），
	// 供「详情」实时滚动查看（增量 log 接口），进程退出后文件仍可读。
	logPath := filepath.Join(u.userRoot, ".teamix", "logs", "services",
		fmt.Sprintf("%s-%s-%d.log", projectName, module, port))
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	var lf *os.File
	if f, err := os.Create(logPath); err == nil {
		lf = f
	}
	out := newLimitedBuffer(32 * 1024)
	var w io.Writer = out
	if lf != nil {
		w = io.MultiWriter(out, lf)
	}
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		return recordFail(fmt.Errorf("start failed: %w", err))
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
		out:       out,
		logPath:   logPath,
	}
	// 清理同 项目/模块 的旧记录（stopped/failed 残留，避免重复）
	for _, old := range svcMgr.list(u.token) {
		if old.Project == projectName && old.Service == module && old.ID != svcID {
			svcMgr.remove(u.token, old.ID)
		}
	}
	svcMgr.add(u.token, rs)
	slog.Info("teamix: service started", "user", u.name, "project", projectName,
		"module", module, "pid", cmd.Process.Pid, "port", port)

	// 进程退出：标记 failed/stopped 并保留（不自动移除——抽屉需显示失败原因、
	// 且"停止后保留可再启动"；重新启动时 startService 会清理同模块旧记录）
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(done)
		if lf != nil {
			_ = lf.Close()
		}
		rs.mu.Lock()
		if rs.Stage != "stopped" { // 手动停止（killProcess 已置 stopped）不覆盖
			rs.Stage = "failed"
			if err != nil {
				rs.Error = err.Error()
			} else {
				rs.Error = "进程已退出"
			}
		}
		rs.mu.Unlock()
		slog.Info("teamix: service exited", "id", svcID, "err", err)
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
	// 手动停止：保留记录（Stage=stopped），抽屉显示"已停止"，可再启动
	svc.mu.Lock()
	svc.Stage = "stopped"
	svc.Error = "已手动停止"
	svc.mu.Unlock()

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
		Output    string `json:"output,omitempty"`
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
		if s.out != nil {
			out[i].Output = s.out.String()
		}
		s.mu.Unlock()
	}
	writeJSON(w, out)
}

// GET /teamix/services/log?id=<svcID>&offset=<bytes> 增量读取服务日志文件：
// 从 offset 起返回新内容，{id, offset(读取后的新位置), data}；进程退出后文件仍可读，
// 配合前端每秒轮询实现「详情」实时滚动。无文件/服务不存在返回空增量（不报错）。
func (ts *TeamixServer) handleServiceLog(w http.ResponseWriter, r *http.Request, u *userSession) {
	id := r.URL.Query().Get("id")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	svc := svcMgr.find(u.token, id)
	if svc == nil || svc.logPath == "" {
		writeJSON(w, map[string]any{"id": id, "offset": offset, "data": ""})
		return
	}
	f, err := os.Open(svc.logPath)
	if err != nil {
		writeJSON(w, map[string]any{"id": id, "offset": offset, "data": "", "error": err.Error()})
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeJSON(w, map[string]any{"id": id, "offset": offset, "data": ""})
		return
	}
	size := fi.Size()
	if int64(offset) >= size {
		writeJSON(w, map[string]any{"id": id, "offset": size, "data": ""})
		return
	}
	buf := make([]byte, size-int64(offset))
	if _, err := f.ReadAt(buf, int64(offset)); err != nil && err != io.EOF {
		writeJSON(w, map[string]any{"id": id, "offset": offset, "data": ""})
		return
	}
	writeJSON(w, map[string]any{"id": id, "offset": size, "data": string(buf)})
}
