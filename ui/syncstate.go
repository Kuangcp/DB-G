package ui

import "db-g/app"

// syncState 是 Git 同步设置、密钥弹窗与发现项的状态。
type syncState struct {
	PrefsPath     string
	Prefs         app.SyncPrefs
	Warn          string
	Result        string
	Busy          bool
	KeySet        bool
	KeyKCV        string
	KeyOpen       bool
	KeyImportOnly bool
	KeyInput      string
	KeyErr        string
	KeyRepoKCV    string
	PendingKind   string
	Findings      []app.SecretFinding
	FindingsOpen  bool
}
