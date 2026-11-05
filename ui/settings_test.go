package ui

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestSettingsViewShowsPerformanceControls(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(a.settingsView, 560, 340)
	tt.Frame()
	for _, want := range []string{"编辑器性能", "行数=", "3k", "12k", "100k", "刷新"} {
		if !tt.HasText(want) {
			t.Fatalf("settings view missing %q: %q", want, tt.Texts())
		}
	}
}

func TestTopBarDropsDebugControls(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	for _, gone := range []string{"3k", "12k", "100k"} {
		if tt.HasText(gone) {
			t.Fatalf("top bar must not show %q any more: %q", gone, tt.Texts())
		}
	}
	if !tt.HasText("设置") {
		t.Fatalf("settings button missing: %q", tt.Texts())
	}
	if !tt.HasText("查找") {
		t.Fatalf("find controls must stay in the toolbar: %q", tt.Texts())
	}
}

func TestSettingsPresetWritesActiveConsole(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "预设")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	tt := ui.NewTester(a.settingsView, 560, 340)
	tt.Frame()
	if err := tt.Click("3k"); err != nil {
		t.Fatal(err)
	}
	want := buildSQL(3_000)
	if got := a.activeText(); got != want {
		t.Fatalf("preset must fill the editor: got %d runes, want %d", len([]rune(got)), len([]rune(want)))
	}
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir()}.Read(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if text != want {
		t.Fatalf("preset must be saved to the console file: got %d runes", len([]rune(text)))
	}
}

func TestSettingsPresetNeedsActiveConsole(t *testing.T) {
	a := newConsoleRoot(t)
	before := a.activeText()
	tt := ui.NewTester(a.settingsView, 560, 340)
	tt.Frame()
	_ = tt.Click("12k")
	if got := a.activeText(); got != before {
		t.Fatalf("preset must do nothing without an active console: %d runes", len([]rune(got)))
	}
}

type fakeSettingsWin struct {
	focuses int
	updates int
	options mygo.WindowOptions
}

func (f *fakeSettingsWin) Focus()        { f.focuses++ }
func (f *fakeSettingsWin) Update(func()) { f.updates++ }

func TestSettingsButtonOpensSingletonWindow(t *testing.T) {
	a := newConsoleRoot(t)
	var created []*fakeSettingsWin
	a.openSettingsWin = func(o mygo.WindowOptions) settingsWin {
		f := &fakeSettingsWin{options: o}
		created = append(created, f)
		return f
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(created) != 1 {
		t.Fatalf("设置 must open exactly one window: %d", len(created))
	}
	if created[0].options.Title != "设置 — db-g" {
		t.Fatalf("window options: %+v", created[0].options)
	}
	// 再点一次：聚焦已有窗口，不新建
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(created) != 1 || created[0].focuses == 0 {
		t.Fatalf("second click must focus the existing window: created=%d focuses=%d", len(created), created[0].focuses)
	}
	// 关窗后清空单例，再点能重开
	a.settingsWin = nil
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(created) != 2 {
		t.Fatalf("closed window must be recreated: %d", len(created))
	}
}

func TestSettingsHasRefreshCompletionsButton(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(a.settingsView, 560, 400)
	tt.Frame()
	if !tt.HasText("刷新补全缓存") {
		t.Fatalf("refresh completions button missing: %q", tt.Texts())
	}
}
