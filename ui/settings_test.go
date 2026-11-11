package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// openSettingsSection 打开设置窗口并切到指定分区。
func openSettingsSection(t *testing.T, a *root, label string) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(a.settingsView, 720, 520)
	tt.Frame()
	if err := tt.Click(label); err != nil {
		t.Fatalf("切到「%s」分区: %v", label, err)
	}
	tt.Frame()
	return tt
}

func TestSettingsViewShowsPerformanceControls(t *testing.T) {
	a := newConsoleRoot(t)
	tt := openSettingsSection(t, a, "开发")
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
	tt := openSettingsSection(t, a, "开发")
	if err := tt.Click("3k"); err != nil {
		t.Fatal(err)
	}
	want := buildSQL(3_000)
	if got := a.activeText(); got != want {
		t.Fatalf("preset must fill the editor: got %d runes, want %d", len([]rune(got)), len([]rune(want)))
	}
	text, err := app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store}.Read(rec.ID)
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
	tt := openSettingsSection(t, a, "开发")
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
	if created[0].options.Title != "设置 — "+app.DisplayName {
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
	tt := openSettingsSection(t, a, "开发")
	if !tt.HasText("刷新补全缓存") {
		t.Fatalf("refresh completions button missing: %q", tt.Texts())
	}
}

func TestSettingsShowsLogPath(t *testing.T) {
	a := newConsoleRoot(t)
	dir := initTestLogs(t)
	tt := openSettingsSection(t, a, "日志")
	if !tt.HasText(dir) {
		t.Fatalf("settings must show the log dir %q: %q", dir, tt.Texts())
	}
	if !tt.HasText("打开日志目录") {
		t.Fatalf("open-log button missing: %q", tt.Texts())
	}
}

// 设置窗口是左右布局：左侧导航切换右侧内容。
func TestSettingsSectionsSwitch(t *testing.T) {
	a := newConsoleRoot(t)
	tt := openSettingsSection(t, a, "日志")
	if !tt.HasText("打开日志目录") {
		t.Fatalf("日志分区内容缺失: %q", tt.Texts())
	}
	if tt.HasText("字号") {
		t.Fatalf("日志分区不该显示编辑器的字号: %q", tt.Texts())
	}
	// 再切回字体
	if err := tt.Click("字体"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("字号") {
		t.Fatalf("字体分区内容缺失: %q", tt.Texts())
	}
}

// 字号改大后立刻应用到编辑器，并落盘。
func TestEditorFontSizeAppliesAndSaves(t *testing.T) {
	a := newConsoleRoot(t)
	a.fontPrefsPath = filepath.Join(t.TempDir(), "fonts.json")
	tt := openSettingsSection(t, a, "字体")
	if got := a.code.state.Font.Size; got != 13 {
		t.Fatalf("默认字号 %v", got)
	}
	if err := tt.Click("Increase"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.code.state.Font.Size; got != 14 {
		t.Fatalf("点 + 之后编辑器字号应为 14，实际 %v", got)
	}
	prefs, warn := app.LoadFontPrefs(a.fontPrefsPath)
	if warn != "" {
		t.Fatal(warn)
	}
	if prefs.Editor.Size != 14 {
		t.Fatalf("没落盘: %+v", prefs)
	}
	// 恢复默认
	if err := tt.Click("恢复默认"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := a.code.state.Font.Size; got != 13 {
		t.Fatalf("恢复默认后字号应为 13，实际 %v", got)
	}
}

// 字体族写进去就会落到编辑器和预览上。
func TestEditorFontFamilyApplies(t *testing.T) {
	a := newConsoleRoot(t)
	a.fontPrefsPath = filepath.Join(t.TempDir(), "fonts.json")
	_ = openSettingsSection(t, a, "字体")
	a.fontPrefs.Editor.Family = "JetBrains Mono"
	a.applyFontPrefs()
	if got := a.code.state.Font.Family; got != "JetBrains Mono" {
		t.Fatalf("字体族没应用: %q", got)
	}
	prefs, _ := app.LoadFontPrefs(a.fontPrefsPath)
	if prefs.Editor.Family != "JetBrains Mono" {
		t.Fatalf("没落盘: %+v", prefs)
	}
}

// 三个区域各有字体控件，功能界面字体落到主题上。
func TestFontSectionCoversThreeAreas(t *testing.T) {
	a := newConsoleRoot(t)
	a.fontPrefsPath = filepath.Join(t.TempDir(), "fonts.json")
	tt := openSettingsSection(t, a, "字体")
	for _, want := range []string{"编辑区", "结果区", "功能界面", "预览"} {
		if !tt.HasText(want) {
			t.Fatalf("字体分区缺少 %q: %q", want, tt.Texts())
		}
	}
	a.fontPrefs.UI = "Noto Sans CJK SC"
	a.applyFontPrefs()
	var got string
	tt = ui.NewTester(func(c *ui.Context) {
		a.applyTheme(c)
		got = c.Theme().Font
		ui.Text(c, "x")
	}, 400, 200)
	tt.Frame()
	if got != "Noto Sans CJK SC" {
		t.Fatalf("功能界面字体没进入主题: %q", got)
	}
	prefs, _ := app.LoadFontPrefs(a.fontPrefsPath)
	if prefs.UI != "Noto Sans CJK SC" {
		t.Fatalf("没落盘: %+v", prefs)
	}
}

// 窗口左下角显示版本号。
func TestSettingsShowsVersion(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(a.settingsView, 720, 520)
	tt.Frame()
	if !tt.HasText(app.Version()) {
		t.Fatalf("缺少版本号 %q: %q", app.Version(), tt.Texts())
	}
}

// 外观分区：默认项齐全，切换模式/强调色/密度即时生效并落盘。
func TestAppearanceSectionControls(t *testing.T) {
	a := newConsoleRoot(t)
	a.themePath = filepath.Join(t.TempDir(), "theme.json")
	tt := openSettingsSection(t, a, "外观")
	for _, want := range []string{"跟随系统", "浅色", "深色", "强调色", "密度", "预览", "恢复默认", "导出主题…", "导入主题…"} {
		if !tt.HasText(want) {
			t.Fatalf("appearance section missing %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.themePrefs.Mode != app.ThemeModeDark {
		t.Fatalf("mode: %+v", a.themePrefs)
	}
	if got, _ := app.LoadThemePrefs(a.themePath); got.Mode != app.ThemeModeDark {
		t.Fatalf("persisted mode: %+v", got)
	}
	if err := tt.Click("强调色：紫"); err != nil {
		t.Fatal(err)
	}
	if a.themePrefs.Accent != "violet" {
		t.Fatalf("accent: %+v", a.themePrefs)
	}
	if err := tt.Click("紧凑"); err != nil {
		t.Fatal(err)
	}
	if a.themePrefs.Density != app.ThemeDensityCompact {
		t.Fatalf("density: %+v", a.themePrefs)
	}
}

// 恢复默认清空覆盖项。
func TestAppearanceReset(t *testing.T) {
	a := newConsoleRoot(t)
	a.themePath = filepath.Join(t.TempDir(), "theme.json")
	a.setThemePrefs(app.ThemePrefs{Mode: "dark", Accent: "violet", Density: "compact", Radius: 10, FontSize: 16})
	tt := openSettingsSection(t, a, "外观")
	tt.SetSize(720, 900)
	tt.Frame()
	if err := tt.Click("恢复默认"); err != nil {
		t.Fatal(err)
	}
	if a.themePrefs != app.DefaultThemePrefs() {
		t.Fatalf("reset: %+v", a.themePrefs)
	}
}

// 主题文件导出/导入往返（对话框之外的实现）。
func TestThemeExportImport(t *testing.T) {
	a := newConsoleRoot(t)
	path := filepath.Join(t.TempDir(), "theme.json")
	a.setThemePrefs(app.ThemePrefs{Mode: "light", Accent: "#7c3aed", Density: "roomy", Radius: 4, FontSize: 15})
	if err := a.exportThemeTo(path); err != nil {
		t.Fatal(err)
	}
	a.setThemePrefs(app.DefaultThemePrefs())
	warn, err := a.importThemeFrom(path)
	if err != nil || len(warn) != 0 {
		t.Fatalf("import: warn=%v err=%v", warn, err)
	}
	want := app.ThemePrefs{Mode: "light", Scheme: app.ThemeSchemeDefault, Accent: "#7c3aed", Density: "roomy", Radius: 4, FontSize: 15}
	if a.themePrefs != want {
		t.Fatalf("imported: %+v want %+v", a.themePrefs, want)
	}
}
