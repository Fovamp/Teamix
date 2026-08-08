//go:build !windows

package serve

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcGroup 让子进程成为新进程组组长（PGID == PID），停止时 killTree 用
// 负 PGID 整组击杀 sh→mvn→java 全部进程——否则只杀 sh 外壳会残留 java 占端口。
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree 杀整棵进程树（Unix: 对进程组发信号，负 PID 即 PGID）。
// 先 SIGTERM 优雅退出，5s 未退出 SIGKILL 强杀。
func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		time.Sleep(5 * time.Second)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}()
}
