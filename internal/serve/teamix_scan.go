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
func analyzeProject(dir string) []teamixconfig.ServiceEntry {
	var out []teamixconfig.ServiceEntry
	out = append(out, analyzeMavenModules(dir)...)
	out = append(out, analyzeNodeProjects(dir)...)
	return out
}

// analyzeMavenModules 解析根 pom.xml 的多模块，含 SpringBootApplication 的为 backend 服务。
func analyzeMavenModules(dir string) []teamixconfig.ServiceEntry {
	var out []teamixconfig.ServiceEntry
	data, err := os.ReadFile(filepath.Join(dir, "pom.xml"))
	if err != nil {
		return out
	}
	for _, m := range mavenModuleRe.FindAllStringSubmatch(string(data), -1) {
		mod := strings.TrimSpace(m[1])
		if mod == "" {
			continue
		}
		modDir := filepath.Join(dir, mod)
		if !hasSpringBootApp(modDir) {
			continue
		}
		out = append(out, teamixconfig.ServiceEntry{
			Name:    mod,
			Type:    "backend",
			Dir:     mod + "/",
			Startup: "mvn spring-boot:run -pl " + mod,
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

// analyzeNodeProjects 识别子目录中含 dev/start script 的 package.json → frontend 服务。
func analyzeNodeProjects(dir string) []teamixconfig.ServiceEntry {
	var out []teamixconfig.ServiceEntry
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "node_modules" || e.Name() == ".git" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "package.json"))
		if err != nil {
			continue
		}
		if devScriptRe.Match(data) {
			out = append(out, teamixconfig.ServiceEntry{
				Name:    e.Name(),
				Type:    "frontend",
				Dir:     e.Name() + "/",
				Startup: "npm run dev",
			})
		}
	}
	return out
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
