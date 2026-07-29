package serve

import (
	"fmt"
	"os/exec"
	"strings"
)

// Impact analysis handler (git diff based).

func (ts *TeamixServer) runImpactAnalysis(currentUser string) string {
	root := ts.workspaceRoot
	if root == "" { root = "." }
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil { return "变更文件获取失败：" + err.Error() }
	changedFiles := strings.Fields(string(out))
	if len(changedFiles) == 0 { return "无未提交的变更文件。" }
	summary := "【变更文件列表】\n"
	summary += "以下文件已被修改（共 " + fmt.Sprint(len(changedFiles)) + " 个）：\n"
	for _, f := range changedFiles { summary += "  - " + f + "\n" }
	summary += "\n请使用 codebase-memory MCP 工具对这些文件中的导出函数/接口进行调用链分析，"
	summary += "然后将分析结果通过 write_file 写入 .teamix/impact-analysis/ 目录下的 JSON 文件，"
	summary += "系统将自动读取该文件并通知相关用户。\n"
	return summary
}

