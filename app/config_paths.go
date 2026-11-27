package app

import "path/filepath"

// ConfigPaths 是 db-g 配置目录下的全部落盘位置。
// 生产从 AppConfigDir() 派生；测试从临时连接库所在目录派生，保证不碰真实用户配置。
type ConfigPaths struct {
	Dir string
}

// DefaultConfigPaths 返回生产配置目录。
func DefaultConfigPaths() ConfigPaths { return ConfigPaths{Dir: AppConfigDir()} }

// ConfigPathsForStore 用连接库路径所在目录作为配置根。
func ConfigPathsForStore(storePath string) ConfigPaths {
	dir := filepath.Dir(storePath)
	if dir == "" {
		dir = "."
	}
	return ConfigPaths{Dir: dir}
}

// Store 返回 connections.json。
func (p ConfigPaths) Store() string { return filepath.Join(p.Dir, "connections.json") }

// Keymap 返回 keymap.json。
func (p ConfigPaths) Keymap() string { return filepath.Join(p.Dir, "keymap.json") }

// Fonts 返回 fonts.json。
func (p ConfigPaths) Fonts() string { return filepath.Join(p.Dir, "fonts.json") }

// Results 返回 results.json。
func (p ConfigPaths) Results() string { return filepath.Join(p.Dir, "results.json") }

// Console 返回 console.json。
func (p ConfigPaths) Console() string { return filepath.Join(p.Dir, "console.json") }

// Theme 返回 theme.json。
func (p ConfigPaths) Theme() string { return filepath.Join(p.Dir, "theme.json") }

// Locale 返回 locale.json。
func (p ConfigPaths) Locale() string { return filepath.Join(p.Dir, "locale.json") }

// AI 返回 ai.json。
func (p ConfigPaths) AI() string { return filepath.Join(p.Dir, "ai.json") }

// Sync 返回 sync.json。
func (p ConfigPaths) Sync() string { return filepath.Join(p.Dir, "sync.json") }

// MetaDir 返回数据源元数据缓存目录。
func (p ConfigPaths) MetaDir() string { return filepath.Join(p.Dir, "meta") }

// HistoryDir 返回 SQL 历史目录。
func (p ConfigPaths) HistoryDir() string { return filepath.Join(p.Dir, "history") }

// LogDir 返回日志目录。
func (p ConfigPaths) LogDir() string { return filepath.Join(p.Dir, "logs") }
