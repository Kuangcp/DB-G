# 快捷键统一管理与改键 设计

日期：2026-11-06
状态：已由用户拍板（覆盖编辑器默认：`Ctrl+D`=删除行、`Ctrl+Y`=复制行；改键 UI 现在就做）

## 问题

1. **Ctrl+C 在编辑器里不生效**。根因是 `ui/result.go` 的
   `if c.Shortcut(ui.Ctrl, ui.KeyC) && !a.code.state.Focused() { … }`：
   `c.Shortcut` 先执行、一旦返回 true 就把键**消费**掉，后面的聚焦判断只是决定要不要
   执行复制——于是编辑器永远收不到 Ctrl+C。
2. 快捷键散在三处各管各的：窗口 `c.Shortcut`（执行/保存/复制单元格）、编辑器内置
   keymap（Ctrl+Z/Y/D…）、工具栏按钮（查找/格式化）。没有统一入口，也没法改键。

## 目标

1. 一张动作表管所有键位，作用域明确，修掉上面那种"抢键"。
2. 键位可改，持久化到 `<config>/db-g/keymap.json`，设置窗口里能录新组合。
3. 按用户习惯覆盖编辑器默认：`Ctrl+Y` = 复制当前行，`Ctrl+D` = 删除当前行/选区。

## 设计

### 动作表（`ui/keymap.go`）

```go
type keyAction struct {
    ID     string            // 持久化键名
    Label  string            // 设置窗口里的名字
    Scope  keyScope          // scopeAlways / scopeEditor / scopeNoEditor
    Def    []chord           // 默认键位（可多个，如 Ctrl+Enter + F5）
    Cmd    myeditor.Command  // 非 NoCommand = 交给编辑器执行
}
```

当前动作：`exec`(Ctrl+Enter, F5) `save`(Ctrl+S) `duplicateLine`(Ctrl+Y)
`deleteLine`(Ctrl+D) `format`(Ctrl+Shift+F) `copyCell`(Ctrl+C, 仅结果区)。

`chord{Mods, Key}` 与字符串 `"Ctrl+Shift+F"` 双向转换（`parseChord`/`formatChord`），
键名表覆盖字母、数字、F1–F12 与常用功能键。

### 分发（`ui/shortcuts.go`）

每帧开头 `dispatchKeys(c)`：

1. **先按作用域过滤，再 `c.Shortcut`** —— 这是修抢键的关键规则。
2. `Cmd != NoCommand` 的动作在这里跳过：编辑器自己处理。
3. 命中的动作交给 `runKeyAction`。

### 编辑器绑定（`applyEditorBindings`）

把动作表翻成 `myeditor.Binding` 灌进 `State.Bindings`：

- 编辑区动作 → 对应命令（`DuplicateLinesDown` / `DeleteLine`），天然顶替编辑器默认键；
- 交给窗口的动作（执行/保存）→ `NoCommand`，编辑器不吞这些键；
- `scopeNoEditor` 的动作（复制单元格）**不碰**编辑器绑定，避免又把 Ctrl+C 关掉。

### 改键

- 存盘只写与默认值不同的动作；空串表示用户特意解绑。
- `setChords` 存盘 + 重建编辑器绑定；同作用域撞键时把原来那个动作解绑（`breakConflicts`）。
- 设置窗口「快捷键」分区：每行 动作 / 当前键位 / 录制 / 恢复默认，外加"全部恢复默认"。
  录制时放一个 `Focusable().Focus().HandleInput(...)` 的框**每帧钉住焦点**，按键都先到它
  （用 `AutoFocus` 不行：它只在元素首次出现的那一帧给焦点）。

## 非目标

- 不做多段和弦（`Ctrl+K Ctrl+0` 那种），mygo 的 `Binding.ThenMods/ThenKey` 目前用不上。
- 不做按作用域的独立键位表（同一个键在不同区域绑不同动作），动作自带作用域即可。
- 不改 mygo 的默认键表：覆盖只发生在 db-g 声明的那几个键上。

## 影响面

`ui/{keymap,shortcuts,keybindings}.go`（新）、`ui/{root,code,result,settings}.go`、
`app/keymap.go`（新，持久化）、设置窗口尺寸 560×340 → 620×560 且整块可滚。

## 验收

- 编辑器里 Ctrl+C 复制选中/当前行；结果区不聚焦编辑器时 Ctrl+C 复制单元格。
- Ctrl+Y 复制当前行、Ctrl+D 删除当前行（不再"选中下一个相同词"）。
- 设置窗口点「录制」→ 按 Ctrl+J → 执行键变成 Ctrl+J，`keymap.json` 落盘，重启仍是新键。
- 同作用域撞键时旧动作自动解绑。
- `go build ./... && go vet ./... && go test -p 1 ./...` 全绿。
