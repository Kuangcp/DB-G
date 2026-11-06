# 主题管理与外观自定义 设计

日期：2026-11-06
状态：已批准并实现（范围见第 9 节）

## 1. 背景与目标

db-g 目前完全使用 mygo 的默认主题：跟随系统深浅色、跟随桌面强调色与字号缩放。
用户提出「主题管理和自定义配置」，目标是：

1. 能显式选择 **跟随系统 / 浅色 / 深色**；
2. 常用外观项可调：**强调色、间距密度、圆角、界面字号**；
3. 修改即时生效、重启保留，不破坏「跟随系统」的默认体验；
4. 设置窗口里能预览、能一键恢复默认。

## 2. 现状（mygo 能力与约束）

- `c.Theme()` 默认主题由 mygo 生成：跟随系统深浅色与桌面设置
  （`c.Preferences()`：`Accent`、`HighContrast`、`TextScale` 等）。
- 自定义方式：复制当前主题 `t := *c.Theme()`，改字段后 `c.SetTheme(&t)`；
  `SetTheme` 作用于整个窗口（根背景与之后构建的控件），需在帧首调用。
- `ui.Themed(c, t)` 只影响一段子树，适合做预览卡片。
- `ui.LightTheme()` / `ui.DarkTheme()` 是内置调色板；mygo 内部把桌面
  `Preferences` 合并进主题（`follow`）的函数**未导出**，因此显式浅/深色模式
  无法自动获得 HighContrast/TextScale 的完整处理，只能自行处理
  `TextScale`（字号缩放）与强调色。
- 主题字段（`ui.Theme`）包含：`Background/Surface/SurfaceHover/SurfacePressed/
  Border/Text/TextMuted/Accent/AccentHover/AccentPressed/AccentText/
  InactiveAccent/Danger/Warning/Success/Selection/Focus/Scrollbar/Radius/
  Spacing/FontSize/Inverse/InverseText/TooltipDelay` 等。
- 现有持久化约定：`<config>/db-g/{connections.json,editor.json,keymap.json,…}`，
  `app` 包负责读写与校验，`ui` 只持有内存态。

## 3. 功能范围（本期）

### 3.1 主题模式

| 选项 | 行为 |
|---|---|
| 跟随系统（默认） | 不调用 `SetTheme`（或只改用户显式覆盖的项），深浅色随系统切换 |
| 浅色 | 以 `LightTheme()` 为底，叠加用户覆盖项 |
| 深色 | 以 `DarkTheme()` 为底，叠加用户覆盖项 |

### 3.2 可调项

| 项 | 范围 | 对应主题字段 |
|---|---|---|
| 强调色 | 默认（系统/主题自带）、蓝/紫/绿/橙/玫红预设、自定义 `#rrggbb` | `Accent`，派生 `AccentHover/AccentPressed/AccentText/Focus/Selection` |
| 间距密度 | 紧凑 3 / 标准 4 / 宽松 5 | `Spacing` |
| 圆角 | 0–12（步长 1，默认 6） | `Radius` |
| 界面字号 | 12–18（默认 14，受桌面 TextScale 影响） | `FontSize` |

非目标（本期不做）：完整逐字段配色（背景/表面/文字各自取色）、按数据源/
按窗口分别配色、动画/材质。可作为后续扩展（见第 8 节）。

主题文件**导入/导出**纳入本期：格式见 4.1，入口在「外观」分区底部。

### 3.3 设置入口

设置窗口新增 **「外观」** 分区（排在「编辑器」前）：

```
外观
┌ 主题：  [跟随系统] [浅色] [深色]
│ 强调色： ●默认 ●蓝 ●紫 ●绿 ●橙 ●玫红  [#______]
│ 密度：  [紧凑] [标准] [宽松]
│ 圆角：  ─────●─────  6
│ 界面字号： ────●────   14
│
│ 预览（实时）：
│  ┌────────────────────────────────┐
│  │ 主按钮   次按钮   徽标   表格行  │
│  └────────────────────────────────┘
│                                          [恢复默认]
```

- 改动落在内存后立即 `applyTheme` + 重画主窗口/设置窗口；写盘防抖（~300ms）。
- 「恢复默认」清空 `theme.json` 并回到跟随系统。
- 预览区直接用全局主题（改动本来就即时生效），再加一行 `ui.Themed` 对照
  （浅/深各半）可选，避免误以为只影响预览。

## 4. 配置模型与持久化

位置：`<config>/db-g/theme.json`（`app.ThemePrefs`，0600），旧文件缺字段按零值，
与 `editor.json`/`keymap.json` 一致。示例：

```json
{
  "mode": "system",          // system | light | dark
  "accent": "default",       // default | blue | violet | green | orange | rose | #rrggbb
  "density": "normal",       // compact | normal | roomy
  "radius": 6,               // 0–12
  "fontSize": 14             // 12–18；0 = 主题默认
}
```

- 读取失败/字段非法：逐项回退默认并记一条 `LogWarn("startup", …)`，不阻断启动。
- 只写与默认不同的字段（与 keymap 的思路一致），文件可读性优先。
- 写入失败：设置窗口内显示一行 `app.LogError` + 静默保留内存设置。

### 4.1 主题文件导入/导出

导出：`文件 → 导出主题…`（保存对话框），内容与 `theme.json` 相同，另带
`"version": 1` 与 `"app": "db-g"` 便于识别：

```json
{ "app": "db-g", "version": 1, "mode": "dark", "accent": "#7c3aed",
  "density": "compact", "radius": 8, "fontSize": 15 }
```

导入：选择 JSON → 校验 `app/version`（缺省按 1）→ 逐字段校验，非法字段
保持当前值并汇总一行「N 项无效，已忽略」；合法字段立即生效并落盘。
导出复用设置窗口的风格（文字按钮 + 保存对话框），失败原因写回该分区提示行。

## 5. 主题计算（`ui/theme_prefs.go`）

```go
// applyTheme 在帧首应用用户主题；跟随系统且无覆盖时不调用 SetTheme。
func (a *root) applyTheme(c *ui.Context) {
    th, set := a.theme.current(c.Theme(), c.Preferences())
    if set {
        c.SetTheme(th)
    }
}
```

`current(base *ui.Theme, p ui.Preferences) (*ui.Theme, bool)` 规则：

1. `mode == system`：
   - 无任何覆盖项 → `return base, false`（完全交给 mygo，保留 HighContrast 等）；
   - 有覆盖项 → 复制 `*base` 再改，`return &t, true`。
2. `mode == light/dark`：`t := *ui.LightTheme()` 或 `DarkTheme()`，然后：
   - `FontSize` = 主题默认 × `p.TextScale`（TextScale 为 0 视为 1）；
   - 强调色为「默认」时用 `p.Accent`（桌面强调色存在时），否则用预设/自定义；
   - 再叠加密度/圆角/字号覆盖。
3. 强调色派生（`accentColors`）：
   - `AccentHover`/`AccentPressed`：向黑/白 `MixOklab` 12%/22%（深色主题反向）；
   - `AccentText`：按 sRGB 相对亮度选 `#111` 或 `#fff`（保证 4.5:1 近似）；
   - `Focus` = `Accent.Alpha(0.55)`；`Selection` = `Accent.Alpha(0.25)`。
4. 非法 hex 在设置窗口即时标红，不写入配置。

帧首调用点：`ui/root.go` 的 `view()` 与 `ui/settings.go` 的 `settingsView()`。
设置窗口与主窗口各自取同一份 `ThemePrefs`。

## 6. 交互细节与边界

- **系统深浅切换**：`mode=system` 且无覆盖时零干预；有覆盖时每帧从新的
  `base` 重新计算，切换即时生效。
- **桌面强调色**：`mode=system` 下「强调色=默认」保留系统行为；显式浅/深色下
  同样优先用 `p.Accent`（桌面有强调色时），避免选择主题就丢掉个性化。
- **TextScale**：显式模式手动乘 `p.TextScale`，不使用未导出的 `follow`；
  HighContrast 在显式模式下不保证（记录在文档与设置提示里）。
- **弹窗/菜单**：`SetTheme` 在帧首调用，模态、菜单、Toast 由窗口主题渲染，
  自动跟随；不需要逐处改颜色（现有代码已经全部用 `c.Theme()`）。
- **热更新**：`a.updateSettingsWindow()` + 主窗口 `win.Update(func(){})` 重画；
  不做重启提示。
- **headless 测试**：`applyTheme` 是纯计算 + 可选 `SetTheme`，可直接断言
  计算结果（颜色、字段），不依赖像素。

## 7. 实施计划（TDD，小步）

1. **app.ThemePrefs**：结构、加载/保存、校验与默认回退（含单测）。
2. **主题计算**：`resolveTheme(base, prefs, prefsDesktop)` 纯函数 + 单测
   （系统无覆盖/set、显式浅深、强调色派生与对比色、密度/圆角/字号）。
3. **接入帧**：`view`/`settingsView` 帧首调用；启动加载；保存防抖。
4. **设置「外观」分区**：模式/强调色/密度/圆角/字号/恢复默认 + 预览。
5. **headless 测试**：切换模式后 `c.Theme()` 的 Dark/Accent/Radius 等；
   恢复默认；非法 hex 不落盘。
6. **手动验收清单**：系统深浅切换、三种模式、强调色对比（浅色/深色各看
   主按钮/选中行/焦点环）、字号与圆角、重启恢复、设置窗口与主窗口一致。

## 8. 风险与后续

- **HighContrast 在显式模式下缺失**：可接受；后续可向 mygo 提 PR，导出
  `Theme.Follow(prefs)`（需要上游合并，和 CellsStyle 一样走 fork/go.work）。
- **设置项太多会稀释「零配置」体验**：默认跟随系统 + 仅外观分区，避免进
  开发者向的逐色编辑。
- **字体**：界面字体族仍由系统/主题决定；编辑器字体族另在「编辑器」分区，
  不合并，避免两套字体概念打架。
- **可选 P3**：完整自定义配色（Background/Surface/Text + 导入导出主题文件），
  用 JSON 文件承载，提供「复制当前主题为 JSON」。

## 9. 已确认（2026-11-06）

1. 自定义深度：**基础外观项**——模式 + 强调色 + 密度 + 圆角 + 界面字号；
   不做逐字段配色。
2. 主题文件：**需要导入/导出**（格式见 4.1）。
3. 强调色：显式浅/深色模式下，「默认」仍采用桌面强调色。
