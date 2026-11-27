package ui

import (
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// settingsState 是设置窗口与全部偏好（字体/结果/控制台/主题/语言/快捷键）的状态。
type settingsState struct {
	Chords        map[string][]chord      // 动作 → 键位（见 keymap.go）
	MouseBindings map[string]ui.Modifiers // 鼠标双击动作 → 修饰键
	KeymapPath    string                  // keymap.json 的位置（空 = 不读盘，测试用）

	FontPrefs        app.FontPrefs // 编辑区/结果区/功能界面字体
	FontPrefsPath    string
	ResultPrefs      app.ResultPrefs
	ResultPrefsPath  string
	ConsolePrefs     app.ConsolePrefs
	ConsolePrefsPath string

	ThemePrefs    app.ThemePrefs
	ThemePath     string
	ThemeSaveSeq  atomic.Uint64
	ThemeHex      string // 外观分区里自定义强调色的输入框
	ThemeHexErr   string
	PaletteLight  app.ThemePalette // 自定义配色的浅色草稿
	PaletteDark   app.ThemePalette // 自定义配色的深色草稿
	PaletteLoaded bool
	PaletteErr    string

	LocalePref      string // 界面语言："" 跟随系统，或 en / zh-Hans
	LocalePrefsPath string

	Section       int                       // 设置窗口选中的分区
	CaptureAction string                    // 正在录制键位的动作
	Notice        map[string]settingsNotice // 分区内联提示（key = 分区）
	Requested     bool
	Win           settingsWin
	OpenWin       func(mygo.WindowOptions) settingsWin // 测试注入；nil = 真实窗口
}
