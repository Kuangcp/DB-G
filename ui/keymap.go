package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
)

// chord 是一个按键组合：修饰键 + 主键（与 mygo 的快捷键模型一致）。
type chord struct {
	Mods ui.Modifiers
	Key  ui.Key
}

// keyScope 是动作生效的范围。
type keyScope int

const (
	scopeAlways   keyScope = iota // 编辑器聚焦时也生效
	scopeEditor                   // 只有编辑区聚焦
	scopeNoEditor                 // 编辑区不聚焦（结果区/侧栏）
)

// keyAction 是一个可绑定的动作。Cmd 非 NoCommand 时由编辑器自己执行（复用
// mygo 的编辑命令）；否则由本包的 dispatchKeys 处理。
type keyAction struct {
	ID     string // 持久化用的键名，别随便改
	Label  string
	Scope  keyScope
	Def    []chord // 默认键位（空 = 默认不绑）
	Cmd    myeditor.Command
	needsE bool // 需要编辑器聚焦才执行（由 Scope 推导，见 applies）
}

// keyActions 是全部可绑定动作，顺序即设置窗口里的顺序。
//
// 注意 Ctrl+D / Ctrl+Y 顶掉了编辑器的默认键（Ctrl+D=选中下一个相同词、
// Ctrl+Y=重做），这是用户明确要求的习惯：重做还有 Ctrl+Shift+Z。
var keyActions = []keyAction{
	{ID: "exec", Label: "执行（选区或当前语句）", Scope: scopeAlways,
		Def: []chord{{ui.Ctrl, ui.KeyEnter}, {0, ui.KeyF5}}},
	{ID: "save", Label: "保存控制台", Scope: scopeAlways,
		Def: []chord{{ui.Ctrl, ui.KeyS}}},
	{ID: "duplicateLine", Label: "复制当前行", Scope: scopeEditor, Cmd: myeditor.DuplicateLinesDown,
		Def: []chord{{ui.Ctrl, ui.KeyY}}},
	{ID: "deleteLine", Label: "删除当前行 / 选区", Scope: scopeEditor, Cmd: myeditor.DeleteLine,
		Def: []chord{{ui.Ctrl, ui.KeyD}}},
	{ID: "format", Label: "格式化 SQL", Scope: scopeEditor,
		Def: []chord{{ui.Ctrl | ui.Shift, ui.KeyF}}},
	{ID: "copyCell", Label: "复制当前单元格", Scope: scopeNoEditor,
		Def: []chord{{ui.Ctrl, ui.KeyC}}},
	{ID: "nextConsole", Label: "切换到最近使用的控制台", Scope: scopeAlways,
		Def: []chord{{ui.Ctrl, ui.KeyTab}}},
}

// applies 报告该动作在「编辑器是否聚焦」时是否生效。
func (k keyAction) applies(editorFocused bool) bool {
	switch k.Scope {
	case scopeEditor:
		return editorFocused
	case scopeNoEditor:
		return !editorFocused
	}
	return true
}

// actionByID 按 id 找动作。
func actionByID(id string) (keyAction, bool) {
	for _, k := range keyActions {
		if k.ID == id {
			return k, true
		}
	}
	return keyAction{}, false
}

// defaultChords 是全部动作的默认键位表（不读盘，测试与启动都用它打底）。
func defaultChords() map[string][]chord {
	out := make(map[string][]chord, len(keyActions))
	for _, act := range keyActions {
		out[act.ID] = act.Def
	}
	return out
}

// loadKeymap 读取用户键位并装进 a.chords；返回警告（读写失败/无法识别的键位）。
// 只读 a.keymapPath 指向的文件，测试把它指到临时目录。
func (a *root) loadKeymap() string {
	a.chords = defaultChords()
	if a.keymapPath == "" {
		return ""
	}
	saved, warn := app.LoadKeymap(a.keymapPath)
	for _, act := range keyActions {
		if raw, ok := saved[act.ID]; ok {
			cs, err := parseChords(raw)
			if err != nil {
				warn = "快捷键 " + act.Label + " 无效（" + err.Error() + "），用默认值"
				a.chords[act.ID] = act.Def
				continue
			}
			a.chords[act.ID] = cs
			continue
		}
		a.chords[act.ID] = act.Def
	}
	return warn
}

// saveKeymap 只把与默认值不同的动作写进 keymap.json。
func (a *root) saveKeymap() {
	saved := map[string]string{}
	for _, act := range keyActions {
		cur := a.chords[act.ID]
		if chordsEqual(cur, act.Def) {
			continue
		}
		saved[act.ID] = formatChords(cur)
	}
	if a.keymapPath == "" {
		return
	}
	if err := app.SaveKeymap(a.keymapPath, saved); err != nil {
		a.consoleErr = "保存快捷键失败: " + err.Error()
	}
}

// setChords 改一个动作的键位：存盘 + 重建编辑器绑定。
func (a *root) setChords(id string, cs []chord) {
	if a.chords == nil {
		a.chords = map[string][]chord{}
	}
	for _, ch := range cs {
		a.breakConflicts(id, ch) // 同作用域撞键：原来的动作让位
	}
	a.chords[id] = cs
	a.saveKeymap()
	a.applyEditorBindings()
}

// resetChords 恢复某个动作（id 为空 = 全部）的默认键位。
func (a *root) resetChords(id string) {
	for _, act := range keyActions {
		if id != "" && act.ID != id {
			continue
		}
		a.chords[act.ID] = act.Def
	}
	a.saveKeymap()
	a.applyEditorBindings()
}

// chordsFor 返回动作当前的键位（可能为空 = 未绑定）。
func (a *root) chordsFor(id string) []chord { return a.chords[id] }

// chordText 是设置窗口里显示的键位文本。
func (a *root) chordText(id string) string {
	cs := a.chords[id]
	if len(cs) == 0 {
		return "未绑定"
	}
	return formatChords(cs)
}

// applyEditorBindings 把键位表翻成编辑器的绑定：
//   - 交给编辑器执行的动作 → 它的命令；
//   - 由窗口处理的动作 → NoCommand，让编辑器不要吞掉这些键（例如 Ctrl+Enter）。
func (a *root) applyEditorBindings() {
	if a.code == nil {
		return
	}
	var out []myeditor.Binding
	for _, act := range keyActions {
		if act.Cmd == myeditor.NoCommand && act.Scope == scopeNoEditor {
			continue // 结果区的动作（复制单元格）不碰编辑器自己的键
		}
		for _, ch := range a.chords[act.ID] {
			out = append(out, myeditor.Binding{Mods: ch.Mods, Key: ch.Key, Command: act.Cmd})
		}
	}
	a.code.state.Bindings = out
}

// chordsEqual 比较两组键位。
func chordsEqual(a, b []chord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// formatChords 把键位拼成 "Ctrl+Enter, F5"。
func formatChords(cs []chord) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, formatChord(c))
	}
	return strings.Join(parts, ", ")
}

// formatChord 把单个键位拼成 "Ctrl+Shift+F"。
func formatChord(c chord) string {
	var b strings.Builder
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}} {
		if c.Mods&m.mod != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(keyName(c.Key))
	return b.String()
}

// parseChords 解析 "Ctrl+Enter, F5"：逗号分隔，空串 = 解绑。
func parseChords(s string) ([]chord, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []chord
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		c, err := parseChord(part)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// parseChord 解析 "Ctrl+Shift+F"。
func parseChord(s string) (chord, error) {
	parts := strings.Split(s, "+")
	var c chord
	for i, raw := range parts {
		p := strings.TrimSpace(raw)
		if p == "" {
			return chord{}, errBadChord(s)
		}
		if i < len(parts)-1 { // 修饰键
			switch strings.ToLower(p) {
			case "ctrl", "control":
				c.Mods |= ui.Ctrl
			case "shift":
				c.Mods |= ui.Shift
			case "alt", "option":
				c.Mods |= ui.Alt
			case "super", "cmd", "command", "win":
				c.Mods |= ui.Super
			default:
				return chord{}, errBadChord(s)
			}
			continue
		}
		key, ok := parseKeyName(p)
		if !ok {
			return chord{}, errBadChord(s)
		}
		c.Key = key
	}
	if c.Key == 0 {
		return chord{}, errBadChord(s)
	}
	return c, nil
}

// keyName 把按键写成可读的名字（与 parseKeyName 对称）。
func keyName(k ui.Key) string {
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + k - ui.KeyA))
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + k - ui.Key0))
	case k >= ui.KeyF1 && k <= ui.KeyF12:
		return "F" + strconv.Itoa(int(k-ui.KeyF1)+1)
	}
	if name, ok := keyNames[k]; ok {
		return name
	}
	return "?"
}

// keyNames 是常用非字母数字键的名字。
var keyNames = map[ui.Key]string{
	ui.KeyEnter: "Enter", ui.KeyEscape: "Escape", ui.KeyBackspace: "Backspace",
	ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert",
	ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
	ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
	ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".",
	ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'",
	ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: `\`,
	ui.KeyBackquote: "`",
}

// parseKeyName 是 keyName 的反向。
func parseKeyName(s string) (ui.Key, bool) {
	if len(s) == 1 {
		r := s[0]
		switch {
		case r >= 'a' && r <= 'z':
			return ui.KeyA + ui.Key(r-'a'), true
		case r >= 'A' && r <= 'Z':
			return ui.KeyA + ui.Key(r-'A'), true
		case r >= '0' && r <= '9':
			return ui.Key0 + ui.Key(r-'0'), true
		}
	}
	if len(s) >= 2 && (s[0] == 'f' || s[0] == 'F') {
		if n, err := strconv.Atoi(s[1:]); err == nil && n >= 1 && n <= 12 {
			return ui.KeyF1 + ui.Key(n-1), true
		}
	}
	for k, name := range keyNames {
		if strings.EqualFold(name, s) {
			return k, true
		}
	}
	return 0, false
}

// errBadChord 是解析失败的统一错误。
type chordError string

func (e chordError) Error() string { return "无法识别的键位: " + string(e) }

func errBadChord(s string) error { return chordError(s) }

// sortedActionIDs 返回动作 id 的稳定顺序（测试与调试用）。
func sortedActionIDs() []string {
	ids := make([]string, 0, len(keyActions))
	for _, k := range keyActions {
		ids = append(ids, k.ID)
	}
	sort.Strings(ids)
	return ids
}
