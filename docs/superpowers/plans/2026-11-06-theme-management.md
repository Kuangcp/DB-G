# 主题管理与外观自定义 实施计划

依据：`docs/superpowers/specs/2026-11-06-theme-management-design.md`
日期：2026-11-06
状态：待执行（按任务顺序，每步 TDD）

## 全局约束

- `CGO_ENABLED=0`；`go test -p 1 ./...`；`go tool mygo vet .`；`gofmt -l .` 干净。
- 不改 mygo（只用导出 API：`SetTheme`、`Themed`、`LightTheme/DarkTheme`、
  `Preferences`）。
- 测试绝不写真实配置：`theme.json` 路径注入 `t.TempDir()`。
- 提交粒度：一个任务一个提交，中文前缀 `feat:` / `ui:` / `feat(theme):`。

## 任务 1：`app.ThemePrefs` 与主题文件

**新增** `app/themeprefs.go`：

```go
type ThemePrefs struct {
    Mode     string `json:"mode"`     // "" | system | light | dark
    Accent   string `json:"accent"`   // "" | default | preset 名 | #rrggbb
    Density  string `json:"density"`  // "" | compact | normal | roomy
    Radius   int    `json:"radius"`   // 0–12，0 = 主题默认
    FontSize int    `json:"fontSize"` // 12–18，0 = 主题默认
}

func ThemePrefsPath(configDir string) string
func LoadThemePrefs(path string) (ThemePrefs, string) // prefs, 警告
func SaveThemePrefs(path string, p ThemePrefs) error
func (p ThemePrefs) Normalize() (ThemePrefs, []string) // 非法项回退 + 说明
func ExportThemePrefs(path string, p ThemePrefs) error
func ImportThemePrefs(path string) (ThemePrefs, []string, error) // 带 version/app 校验
```

- 默认值：`Mode=""`（等同 system）、`Accent=""`（等同 default）、
  `Density=""`（normal）、`Radius=0`、`FontSize=0`。
- 只写非默认字段（`omitempty`），与 keymap 的“只存差异”一致。
- 导入校验 `app=="db-g"`（缺省容忍）与 `version<=1`；非法字段忽略并返回说明。
- 导出文件 0600，内容含 `app/version`。

**测试**（`app/themeprefs_test.go`）：
`TestThemePrefsRoundTrip`、`TestThemePrefsNormalizeInvalid`、
`TestThemePrefsExportImport`、`TestThemePrefsImportRejectsOtherApp`、
`TestThemePrefsImportBadVersion`。

## 任务 2：主题计算纯函数

**新增** `ui/theme_prefs.go`：

```go
type themeResolved struct {
    Theme *ui.Theme
    Apply bool // 是否需要 SetTheme（system + 无覆盖 = false）
}

func resolveTheme(base *ui.Theme, p app.ThemePrefs, desk ui.Preferences) themeResolved
func accentPalette(accent ui.Color, dark bool) (hover, pressed, text ui.Color)
```

规则（对应设计第 5 节）：
- system：无覆盖返回 `{base,false}`；有覆盖复制 base 后改。
- light/dark：`ui.LightTheme()/DarkTheme()` 打底；`FontSize *= TextScale`；
  `Accent` 取 `desk.Accent`（A != 0）或预设/自定义；派生 hover/pressed/text/focus/selection。
- 密度 → `Spacing` 3/4/5；圆角/字号覆盖；非法值忽略。
- 自定义 hex 用 `ui.Hex` 前先校验格式，避免 panic。

**测试**（`ui/theme_prefs_test.go`）：
`TestResolveThemeSystemNoOverrides`、`TestResolveThemeExplicitLightDark`、
`TestResolveThemeAccentDerivation`（含亮色强调色选深色文字）、
`TestResolveThemeDensityRadiusFontSize`、`TestResolveThemeTextScale`、
`TestResolveThemeDesktopAccentUsedWhenDefault`。

## 任务 3：帧接入与持久化

- `root` 增加字段：`themePrefs app.ThemePrefs`、`themePath string`、
  `themeSaveSeq int`（防抖用）、`themeWarn string`。
- 启动：`newRoot` 只设默认；`main.go` / 生产初始化读 `app.ThemePrefsPath(configDir)`；
  测试通过 setter 注入路径（参照 `keymapPath`/`editorPrefsPath`）。
- `view()` 与 `settingsView()` 首行调用 `a.applyTheme(c)`。
- 修改入口 `setThemePrefs(p ThemePrefs)`：Normalize → 赋值 → 记日志 →
  防抖保存（`win.Update` 后 300ms，或简化为每帧变化即写；测试走同步写）→
  `updateSettingsWindow()` + 主窗口重画。
- 保存失败：`themeWarn` 显示在「外观」分区，日志 `LogError("startup", "theme save failed", …)`。

**测试**：`TestApplyThemeSystemKeepsBase`、`TestApplyThemeDarkSetsTheme`（headless
拿不到 c.Theme 时直接测 `a.resolvedTheme`/字段）、`TestThemePrefsPersist` 用
`t.TempDir()` 路径，重启式重载校验。

## 任务 4：设置窗口「外观」分区

`ui/settings.go`：
- `settingsSections()` 最前面加 `{"外观", a.appearanceSection}`。
- 控件：
  - 模式：`ui.Segmented(c, &idx, "跟随系统", "浅色", "深色")`
  - 强调色：色板（默认/蓝/紫/绿/橙/玫红）用 `iconButtonPlain` + 选中描边，
    自定义 hex `TextInput`，非法即时红字（不应用）。
  - 密度：Segmented 紧凑/标准/宽松。
  - 圆角/字号：`ui.Slider`（或 −/+ 按钮，视 mygo 易用性定）。
  - 预览：一行主按钮/次按钮/徽标 + 两行表格样式（直接吃全局主题）。
  - 底部：`恢复默认`、`导出主题…`、`导入主题…` + 一行提示（`themeWarn`/导入结果）。
- 导入/导出复用 `mygo.Dialog.Save/Open`，后台执行 + `win.Update` 回投（照连接档案）。

**测试**：headless 切换模式/强调色后 `a.themePrefs` 更新且 `theme.json` 生成；
`TestAppearanceSectionShowsPreview`（`tt.HasText`）；恢复默认清空文件。

## 任务 5：文档与验收

- `Readme.md`：功能清单加「外观：模式/强调色/密度/圆角/字号，主题导入导出」。
- `TODO.md`：划掉对应项（若有）。
- 手动验收清单（写进设计文档或提交说明）：
  1. 系统深浅切换（三种模式各看一次）；
  2. 强调色在浅/深与选中行/焦点环上的对比；
  3. 密度/圆角/字号即时生效，编辑器字体不受影响；
  4. 重启后外观保留；恢复默认可逆；
  5. 导出→改坏→导入，非法字段被忽略且提示可见。

## 验收命令

```bash
CGO_ENABLED=0 go1.27 build ./... && CGO_ENABLED=0 go1.27 vet ./...
CGO_ENABLED=0 go1.27 test -p 1 ./...
go tool mygo vet .
gofmt -l .
```
