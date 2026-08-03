package serve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"reasonix/internal/teamixconfig"
)

// 项目自动分析器：扫描公共区克隆的项目，识别模块（services）写回 projects.yaml。
// 第一版支持：
//   - Java Maven 多模块（根 pom.xml 的 <modules> + 模块内 @SpringBootApplication 启动类）
//   - Node 子项目（子目录 package.json 含 dev script）
//   - 端口：尝试从 application.yml / application.properties 识别 server.port

var (
	mavenModuleRe   = regexp.MustCompile(`(?s)<module>\s*([^<]+?)\s*</module>`)
	springBootRe    = regexp.MustCompile(`@SpringBootApplication`)
	devScriptRe     = regexp.MustCompile(`"(dev|start)"\s*:`)
	ymlPortRe       = regexp.MustCompile(`(?m)^\s*port:\s*["']?(\d+)["']?\s*$`)
	propPortRe      = regexp.MustCompile(`(?m)^\s*server\.port\s*=\s*(\d+)`)
)

// analyzeProject 扫描项目目录，识别模块清单。
// 支持：根目录 Maven 多模块、子目录独立 Maven 项目（如 JeecgBoot 仓库的 jeecg-boot/）、
// 根/子目录 Node 前端。
func analyzeProject(dir string) []teamixconfig.ServiceEntry {
	var out []teamixconfig.ServiceEntry
	seenDirs := map[string]bool{}
	add := func(s teamixconfig.ServiceEntry) {
		if s.Dir == "" || seenDirs[s.Dir] {
			return
		}
		seenDirs[s.Dir] = true
		out = append(out, s)
	}

	rootPom := filepath.Join(dir, "pom.xml")
	rootHasPom := fileExists(rootPom)

	// 1. 根目录 Maven 多模块
	if rootHasPom {
		for _, m := range mavenModules(rootPom) {
			modDir := filepath.Join(dir, m)
			if !hasSpringBootApp(modDir) {
				continue
			}
			add(teamixconfig.ServiceEntry{
				Name: m, Type: "backend", Dir: m + "/",
				Startup: "mvn spring-boot:run -pl " + m, Port: detectPort(modDir),
			})
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if e.Name() == "node_modules" || e.Name() == ".git" {
			continue
		}
		// 2. 子目录独立 Maven 项目（根无 pom，或该子目录不在根 modules 内）
		subPom := filepath.Join(sub, "pom.xml")
		if fileExists(subPom) && (!rootHasPom || !mavenModules(rootPom).Contains(e.Name())) {
			for _, m := range mavenModules(subPom) {
				modDir := filepath.Join(sub, m)
				if !hasSpringBootApp(modDir) {
					continue
				}
				add(teamixconfig.ServiceEntry{
					Name: m, Type: "backend", Dir: e.Name() + "/" + m + "/",
					Startup: "mvn spring-boot:run -pl " + m, Port: detectPort(modDir),
				})
			}
		}
		// 3. Node 前端（package.json 含 dev/start script）
		if hasDevScript(filepath.Join(sub, "package.json")) {
			add(teamixconfig.ServiceEntry{
				Name: e.Name(), Type: "frontend", Dir: e.Name() + "/",
				Startup: "npm run dev",
			})
		}
	}
	return out
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

type stringList []string

func (l stringList) Contains(s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// mavenModules 解析 pom.xml 的 <modules> 列表。
func mavenModules(pomPath string) stringList {
	data, err := os.ReadFile(pomPath)
	if err != nil {
		return nil
	}
	var out stringList
	for _, m := range mavenModuleRe.FindAllStringSubmatch(string(data), -1) {
		mod := strings.TrimSpace(m[1])
		if mod != "" {
			out = append(out, mod)
		}
	}
	return out
}

// analyzeMavenModules 解析根 pom.xml 的多模块，含 SpringBootApplication 的为 backend 服务。
func analyzeMavenModules(dir string) []teamixconfig.ServiceEntry {
	rootPom := filepath.Join(dir, "pom.xml")
	var out []teamixconfig.ServiceEntry
	for _, m := range mavenModules(rootPom) {
		modDir := filepath.Join(dir, m)
		if !hasSpringBootApp(modDir) {
			continue
		}
		out = append(out, teamixconfig.ServiceEntry{
			Name:    m,
			Type:    "backend",
			Dir:     m + "/",
			Startup: "mvn spring-boot:run -pl " + m,
			Port:    detectPort(modDir),
		})
	}
	return out
}

// hasSpringBootApp 递归查找模块目录内带 @SpringBootApplication 注解的 Java 文件。
func hasSpringBootApp(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if d.Name() == "target" || d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".java") {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil && springBootRe.Match(data) {
			found = true
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

// detectPort 尝试从 application.yml / application.properties 识别 server.port。
func detectPort(modDir string) int {
	for _, f := range []string{"application.yml", "application.yaml", "application.properties"} {
		path := filepath.Join(modDir, "src", "main", "resources", f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if m := ymlPortRe.FindSubmatch(data); m != nil {
			return atoiSafe(string(m[1]))
		}
		if m := propPortRe.FindSubmatch(data); m != nil {
			return atoiSafe(string(m[1]))
		}
	}
	return 0
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
