package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"reasonix/internal/teamixconfig"
)

// 项目自动分析器 v2：递归扫描整个目录树。
//   - 启动类识别：文件名约定 *Application.java + 读文件内容验证 @SpringBootApplication
//     （Spring Boot 主类惯例命名；只读小文件，不整树读内容）
//   - 端口：模块根下递归查找 application*.yml / application*.properties 的 server.port
//   - 前端：package.json 含 dev/start script
//
// 不依赖 pom.xml 的 <modules> 声明，任何嵌套布局（如 JeecgBoot 单仓库 jeecg-boot/）都能扫全。

var (
	springBootRe = regexp.MustCompile(`@SpringBootApplication`)
	devScriptRe  = regexp.MustCompile(`"(dev|start)"\s*:`)
	ymlPortRe    = regexp.MustCompile(`(?m)^\s*port:\s*["']?(\d+)["']?\s*$`)
	propPortRe   = regexp.MustCompile(`(?m)^\s*server\.port\s*=\s*(\d+)`)
	skipScanDirs = map[string]bool{
		".git": true, ".idea": true, ".vscode": true, "node_modules": true,
		"target": true, "dist": true, "build": true, "out": true,
	}
	// 前端子项目的噪音目录（测试/示例/文档），不作为独立模块
	skipFrontendDirs = map[string]bool{
		"tests": true, "test": true, "docs": true, "examples": true, "mock": true, "scripts": true,
	}
)

// analyzeProject 扫描项目目录，识别全部模块（backend 启动类 + frontend）。
// 单遍遍历：启动类识别、端口检测、前端识别共用一次 WalkDir，端口不再
// 对每个模块根做第二次整树遍历（旧实现双重遍历，JeecgBoot 等大仓库极慢）。
//   - 启动类：*Application.java + @SpringBootApplication 注解（跳过 src/test）
//   - 端口：application*.yml / application*.properties 的 server.port（按模块根归组）
//   - 前端：package.json 含 dev/start script
func analyzeProject(dir string) []teamixconfig.ServiceEntry {
	apps := map[string]bool{} // moduleRoot -> 发现启动类
	ports := map[string]int{} // moduleRoot -> port
	portSeen := map[string]bool{}

	var frontends []teamixconfig.ServiceEntry
	seen := map[string]bool{}
	frontendAdd := func(s teamixconfig.ServiceEntry) {
		if s.Dir == "" || seen[s.Dir] {
			return
		}
		seen[s.Dir] = true
		frontends = append(frontends, s)
	}

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipScanDirs[d.Name()] {
				return filepath.SkipDir
			}
			// 跳过 src/test：测试用的 *Application.java 不是可启动服务，也减少遍历量
			if d.Name() == "test" && filepath.Base(filepath.Dir(path)) == "src" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, "Application.java"):
			if data, err := os.ReadFile(path); err == nil && springBootRe.Match(data) {
				apps[moduleRootOf(path)] = true
			}
		case strings.HasPrefix(name, "application") &&
			(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".properties")):
			if data, err := os.ReadFile(path); err == nil {
				root := moduleRootOf(path)
				if !portSeen[root] {
					if m := ymlPortRe.FindSubmatch(data); m != nil {
						ports[root] = atoiSafe(string(m[1]))
						portSeen[root] = true
					} else if m := propPortRe.FindSubmatch(data); m != nil {
						ports[root] = atoiSafe(string(m[1]))
						portSeen[root] = true
					}
				}
			}
		case name == "package.json":
			if hasDevScript(path) {
				projDir := filepath.Dir(path)
				rel, _ := filepath.Rel(dir, projDir)
				// 相对路径任意一段命中噪音目录（如 tests/server）→ 不作为模块
				skip := false
				for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
					if skipFrontendDirs[seg] {
						skip = true
						break
					}
				}
				if skip {
					return nil
				}
				frontendAdd(teamixconfig.ServiceEntry{
					Name: filepath.Base(projDir), Type: "frontend",
					Dir:     filepath.ToSlash(rel) + "/",
					Startup: "npm run dev",
				})
			}
		}
		return nil
	})

	out := make([]teamixconfig.ServiceEntry, 0, len(apps)+len(frontends))
	for root := range apps {
		rel, _ := filepath.Rel(dir, root)
		modName := filepath.Base(root)
		out = append(out, teamixconfig.ServiceEntry{
			Name:    modName,
			Type:    "backend",
			Dir:     filepath.ToSlash(rel) + "/",
			Startup: "mvn spring-boot:run -pl " + modName,
			Port:    ports[root],
		})
	}
	out = append(out, frontends...)
	// 稳定输出：按名称排序，避免 map 遍历顺序抖动导致前端列表乱跳
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// moduleRootOf 从启动类文件路径向上找 src 的父目录（Maven 模块根）。
// .../src/main/java/com/x/App.java → src 的父目录。
func moduleRootOf(javaFilePath string) string {
	p := filepath.Dir(javaFilePath)
	for {
		if filepath.Base(p) == "src" {
			return filepath.Dir(p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Dir(javaFilePath)
		}
		p = parent
	}
}

// detectPortRecursive 已废弃：端口检测并入 analyzeProject 单遍遍历
// （旧实现每发现一个启动类就对模块根再整树遍历，大仓库极慢）。

// hasDevScript 检查 package.json 是否含 dev/start script（判定前端模块）。
func hasDevScript(packageJSONPath string) bool {
	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return false
	}
	return devScriptRe.Match(data)
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
