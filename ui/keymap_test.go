package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// focusEditor 打开一个控制台、把编辑器聚焦。
func focusEditor(t *testing.T, text string) (*root, *ui.Tester) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "键位")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText(text)
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if err := tt.Click("编辑器"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return a, tt
}

func TestChordParseFormatRoundTrip(t *testing.T) {
	for _, s := range []string{"Ctrl+Enter", "F5", "Ctrl+Shift+F", "Ctrl+Alt+Delete", "Shift+Space"} {
		c, err := parseChord(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if got := formatChord(c); got != s {
			t.Fatalf("%s → %s", s, got)
		}
	}
	if _, err := parseChord("Hyper+Q"); err == nil {
		t.Fatal("不认识的修饰键应报错")
	}
	if _, err := parseChord("Ctrl+"); err == nil {
		t.Fatal("缺主键应报错")
	}
	cs, err := parseChords("Ctrl+Enter, F5")
	if err != nil || len(cs) != 2 {
		t.Fatalf("%v %v", cs, err)
	}
	if got, err := parseChords("  "); err != nil || len(got) != 0 {
		t.Fatalf("空串 = 解绑: %v %v", got, err)
	}
}

// 编辑器里的 Ctrl+C 必须真的复制（结果区抢键的老 bug）。
func TestCtrlCCopiesInEditor(t *testing.T) {
	_, tt := focusEditor(t, "SELECT 42")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != "SELECT 42" {
		t.Fatalf("Ctrl+C 应当复制全文，实际 %q", got)
	}
}

// Ctrl+Y 复制当前行。
func TestCtrlYDuplicatesLine(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1")
	tt.Key(ui.Ctrl, ui.KeyY)
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 1\nSELECT 1" {
		t.Fatalf("Ctrl+Y 应复制当前行，实际 %q", got)
	}
}

// Ctrl+D 删除当前行（有选区时删选区所在的整行）。
func TestCtrlDDeletesLine(t *testing.T) {
	a, tt := focusEditor(t, "one\ntwo")
	a.setSelection(0, 0) // 光标放在第一行
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyD)
	tt.Frame()
	if got := a.code.Text(); got != "two" {
		t.Fatalf("Ctrl+D 应删掉当前行，实际 %q", got)
	}
}

// Ctrl+D 不再触发编辑器的「选中下一个相同词」。
func TestCtrlDNoLongerSelectsNextMatch(t *testing.T) {
	a, tt := focusEditor(t, "abc abc")
	tt.Key(ui.Ctrl, ui.KeyD)
	tt.Frame()
	if got := a.code.Text(); got == "abc abc" {
		t.Fatal("Ctrl+D 应当删除行，而不是选中下一个匹配")
	}
}

// 改键：写进 keymap.json，编辑器绑定跟着变，新键立即生效。
func TestRemapPersistsAndRebinds(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1")
	a.keymapPath = filepath.Join(t.TempDir(), "keymap.json")

	a.setChords("duplicateLine", []chord{{ui.Ctrl | ui.Shift, ui.KeyY}})
	tt.Frame()

	if _, err := os.Stat(a.keymapPath); err != nil {
		t.Fatalf("应写出 keymap.json: %v", err)
	}
	saved, warn := app.LoadKeymap(a.keymapPath)
	if warn != "" || saved["duplicateLine"] != "Ctrl+Shift+Y" {
		t.Fatalf("存盘内容: %v %q", saved, warn)
	}

	// 新的组合生效，旧的不再生效。
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyY)
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 1\nSELECT 1" {
		t.Fatalf("新键应复制行，实际 %q", got)
	}
	a.setActiveText("SELECT 2")
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyY)
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 2" {
		t.Fatalf("旧键应当失效，实际 %q", got)
	}
}

// 设置窗口里录制按键：点「录制」→ 按 Ctrl+J → 存进文件。
func TestRecordChordInSettings(t *testing.T) {
	a := newConsoleRoot(t)
	a.keymapPath = filepath.Join(t.TempDir(), "keymap.json")
	tt := ui.NewTester(a.settingsView, 560, 500)
	tt.Frame()
	if err := tt.Click("录制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.captureAction == "" {
		t.Fatal("点录制后应进入录制状态")
	}
	if !tt.HasText("按下新组合") {
		t.Fatalf("录制提示没出来: %q", tt.Texts())
	}
	tt.Key(ui.Ctrl, ui.KeyJ)
	tt.Frame()
	if a.captureAction != "" {
		t.Fatal("按键后应退出录制")
	}
	if got := a.chordsFor("exec"); len(got) != 1 || got[0] != (chord{ui.Ctrl, ui.KeyJ}) {
		t.Fatalf("第一个动作应绑到 Ctrl+J: %v", got)
	}
	saved, _ := app.LoadKeymap(a.keymapPath)
	if saved["exec"] != "Ctrl+J" {
		t.Fatalf("没存盘: %v", saved)
	}
}

// 同作用域撞键时，把原来那个动作解绑，避免两个动作抢一个键。
func TestRemapBreaksConflict(t *testing.T) {
	a := newConsoleRoot(t)
	a.keymapPath = filepath.Join(t.TempDir(), "keymap.json")
	a.setChords("duplicateLine", []chord{{ui.Ctrl, ui.KeyD}}) // 和 deleteLine 撞
	if got := a.chordsFor("deleteLine"); len(got) != 0 {
		t.Fatalf("撞键后 deleteLine 应被解绑: %v", got)
	}
}

// 执行键从编辑器手里拿走：Ctrl+Enter 不换行，而是触发执行。
func TestExecChordStillTakenFromEditor(t *testing.T) {
	a, tt := focusEditor(t, "SELECT 1")
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if got := a.code.Text(); got != "SELECT 1" {
		t.Fatalf("Ctrl+Enter 不该改正文: %q", got)
	}
	// 真的执行了（execPending 当帧就被消费掉，结果在控制台自己的运行态里）
	for range 200 {
		if rt := a.currentRuntime(); rt.Result() != nil || rt.Err() != "" {
			if rt.Err() != "" {
				t.Fatalf("执行失败: %s", rt.Err())
			}
			return
		}
		tt.Frame()
	}
	t.Fatal("Ctrl+Enter 没有执行")
}
