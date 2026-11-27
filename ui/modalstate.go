package ui

import (
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
)

// modalState 是各类弹窗/浮层/面板的瞬态状态（不含设置窗口与连接表单）。
type modalState struct {
	TabMenuAction string // "close" / "rename" / "delete"
	TabMenuTarget string

	Kind   string // "" / "rename" / "delete" / "ws-new" / "ws-rename" / "ws-delete"
	Target string
	Text   string
	Err    string

	FolderKind   string // "" / "new" / "rename" / "delete"
	FolderTarget string
	FolderText   string
	FolderErr    string

	ReloadConfirmOpen bool
	ReloadConsoleID   string
	ReloadPausedID    string

	DDLOpen  bool
	DDLTitle string
	DDLText  string
	DDLState *myeditor.State // 只读高亮编辑器（惰性创建）
	DDLConn  app.ConnProfile // DDL 查询用的连接

	StatsOpen     bool
	StatsLoading  bool
	StatsTitle    string
	StatsKind     string
	Stats         *engine.ObjectStats
	StatsErr      string
	StatsSQLOpen  bool
	StatsSQLState *myeditor.State // 统计 SQL 的只读高亮编辑器（惰性创建）

	HistoryOpen bool

	PaletteOpen    bool
	PaletteQuery   string
	PaletteJumpID  string
	PaletteJumpSel [2]int

	GuardOpen    bool
	GuardBlocked bool
	GuardRisks   []app.SQLRisk
}
