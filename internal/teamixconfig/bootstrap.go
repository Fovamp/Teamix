package teamixconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// 全局工作区初始模板。架构师无需手写：首次启动（或首次指向新工作区）时自动生成，
// 已存在的文件不会被覆盖，后续由架构师/Web 配置中心维护。
const (
	globalConfigTemplate = `# Teamix 全局公共配置（架构师维护，Web 配置中心可改）
teamix:
  name: "Teamix Cloud"
  # 公共默认模型；用户私有配置 preferences.model 可覆盖
  default_model: "deepseek-v3"
`

	globalUsersTemplate = `# 用户白名单。空列表 = 开放模式（任何昵称都能登录）。
# role: architect（配置管理/全局 MCP/部署）| developer
# users:
#   - name: chisato
#     role: architect
#   - name: alice
#     role: developer
users: []
`

	globalProjectsTemplate = `# 项目清单（项目选择页展示）。每个项目是一个 git 仓库。
# projects:
#   - name: mall-system
#     git: "git@github.com:team/mall-system.git"
#     description: "电商系统"
#     services:
#       - name: mall-gateway
#         type: backend
#         dir: mall-gateway/
#         startup: "mvn spring-boot:run -pl mall-gateway"
#         port: 8080
projects: []
`
)

// EnsureGlobalWorkspace 初始化全局工作区结构（幂等）：
//   - 创建 .teamix/、.teamix/notifications/、.teamix/workflows/、.reasonix/ 目录；
//   - 缺失时写入 .teamix/config.yaml / users.yaml / projects.yaml 模板。
//
// 已存在的文件一律不覆盖，保证不会破坏架构师已有配置。
func EnsureGlobalWorkspace(root string) error {
	if root == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(root, ".teamix"),
		filepath.Join(root, ".teamix", "notifications"),
		filepath.Join(root, ".teamix", "workflows"),
		// Reasonix 侧 Agent 基础设施：skills/mcp/capabilities/secrets/commands
		filepath.Join(root, ".reasonix"),
		filepath.Join(root, ".reasonix", "capabilities"),
		filepath.Join(root, ".reasonix", "secrets"),
		filepath.Join(root, ".reasonix", "skills"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	files := []struct{ path, content string }{
		{filepath.Join(root, ".teamix", "config.yaml"), globalConfigTemplate},
		{filepath.Join(root, ".teamix", "users.yaml"), globalUsersTemplate},
		{filepath.Join(root, ".teamix", "projects.yaml"), globalProjectsTemplate},
	}
	for _, f := range files {
		if _, err := os.Stat(f.path); os.IsNotExist(err) {
			if err := os.WriteFile(f.path, []byte(f.content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", f.path, err)
			}
		}
	}
	return nil
}
