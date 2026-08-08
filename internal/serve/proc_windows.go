//go:build windows

package serve

import (
	"os/exec"
	"strconv"
)

// setProcGroup：Windows 无进程组概念，停止时用 taskkill /T 杀树，无需额外设置。
func setProcGroup(cmd *exec.Cmd) {}

// killTree 杀整棵进程树（Windows: taskkill /T，含 java/mvn 等孙进程，防占端口残留）。
func killTree(pid int) {
	_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}
