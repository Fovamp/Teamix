//go:build windows

package serve

import "golang.org/x/sys/windows/registry"

// readUserEnv 从 Windows 用户注册表（HKCU\Environment）读取环境变量——
// 权威值实时可读，不依赖进程环境继承（teamix.exe 从旧终端/IDE 启动时
// 也能拿到用户刚配的 MAVEN_HOME / PATH）。
func readUserEnv(name string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}
