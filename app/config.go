package app

import (
	"os"
	"path/filepath"
)

// 配置目录名：正式发布构建用 db-g，开发构建用 db-g.dev，避免 go run
// 动到用户真实的连接、控制台与历史。
const (
	releaseConfigDirName = "db-g"
	devConfigDirName     = "db-g.dev"
)

// ConfigDirName 返回当前构建使用的配置目录名。
func ConfigDirName() string {
	if ReleaseBuild() {
		return releaseConfigDirName
	}
	return devConfigDirName
}

// AppConfigDir 返回当前构建的配置目录（遵循 XDG_CONFIG_HOME）。
func AppConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ConfigDirName()
	}
	return filepath.Join(dir, ConfigDirName())
}
