package app

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealPath 在系统文件管理器中定位文件；目录不存在或没有可用命令时返回错误。
func RevealPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", "/select,", path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
