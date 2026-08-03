package serve

import (
	"os"
	"path/filepath"
	"regexp"
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
func analyzeProject(dir string) []teamixconfig.ServiceEntry {
	var out []teamixconfig.ServiceEntry
	seen := map[string]bool{}
	add := func(s teamixconfig.ServiceEntry) {
		if s.Dir == "" || seen[s.Dir] {
			return
		}
		seen[s.Dir] = true
		out = append(out, s)
	}

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipScanDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Spring Boot 启动类：*Application.java + @SpringBootApplication 注解
		if strings.HasSuffix(d.Name(), "Application.java") {
			if data, err := os.ReadFile(path); err == nil && springBootRe.Match(data) {
				modRoot := moduleRootOf(path)
				rel, _ := filepath.Rel(dir, modRoot)
				modName := filepath.Base(modRoot)
				add(teamixconfig.ServiceEntry{
					Name:    modName,
					Type:    "backend",
					Dir:     filepath.ToSlash(rel) + "/",
					Startup: "mvn spring-boot:run -pl " + modName,
					Port:    detectPortRecursive(modRoot),
				})
			}
			return nil
		}
		// Node 前端：package.json 含 dev/start script（跳过测试/示例/文档等噪音子项目）
		if d.Name() == "package.json" {
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
				add(teamixconfig.ServiceEntry{
					Name: filepath.Base(projDir), Type: "frontend",
					Dir:     filepath.ToSlash(rel) + "/",
					Startup: "npm run dev",
				})
			}
		}
		return nil
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

// detectPortRecursive 在模块根下递归查找 application*.yml / properties 的 server.port。
func detectPortRecursive(modRoot string) int {
	found := 0
	filepath.WalkDir(modRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipScanDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "application") &&
			(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".properties")) {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if m := ymlPortRe.FindSubmatch(data); m != nil {
				found = atoiSafe(string(m[1]))
				return filepath.SkipAll
			}
			if m := propPortRe.FindSubmatch(data); m != nil {
				found = atoiSafe(string(m[1]))
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

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
