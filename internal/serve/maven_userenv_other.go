//go:build !windows

package serve

// readUserEnv 非 Windows 平台：无 HKCU 注册表，直接返回空（走 PATH/环境变量）。
func readUserEnv(name string) string {
	return ""
}
