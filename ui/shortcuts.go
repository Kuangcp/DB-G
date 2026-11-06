package ui

import (
	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"
)

// dispatchKeys 在每帧开头统一处理窗口级快捷键。
//
// 两个要点：
//  1. 动作先按作用域过滤，再注册 c.Shortcut —— c.Shortcut 一旦返回 true 就
//     把键消费掉，所以「判断能不能用」必须发生在注册之前（历史 bug：结果区的
//     Ctrl+C 把编辑器的复制吃掉了）。
//  2. 交给编辑器执行的动作（Cmd != NoCommand）这里不注册，由编辑器自己的
//     keymap 处理（applyEditorBindings 已经把键位同步过去）。
func (a *root) dispatchKeys(c *ui.Context) {
	editorFocused := a.code != nil && a.code.state.Focused()
	for _, act := range keyActions {
		if act.Cmd != myeditor.NoCommand || !act.applies(editorFocused) {
			continue
		}
		for _, ch := range a.chords[act.ID] {
			if !c.Shortcut(ch.Mods, ch.Key) {
				continue
			}
			a.runKeyAction(c, act.ID)
			break
		}
	}
}

// runKeyAction 执行一个窗口级动作。
func (a *root) runKeyAction(c *ui.Context, id string) {
	switch id {
	case "exec":
		a.execPending = true
	case "save":
		_ = a.flushActive()
	case "format":
		a.formatActive()
	case "copyCell":
		a.copyCurrentCell(c, a.currentRuntime().Result())
	case "nextConsole":
		a.switchToRecentConsole()
	case "toggleResults":
		a.resultsHidden = !a.resultsHidden
	case "viewDDL":
		if a.hasSelObj {
			a.showDDL(a.selConn, a.selObj)
		}
	}
}

// captureKey 是设置窗口录制框的输入回调：录制中把按下的组合记到动作上。
// 录制框在录制时拿焦点（AutoFocus），所以这里能收到任意键；返回 true 表示
// 这个键被吃掉了，不会继续传给后面的控件。
func (a *root) captureKey(ev ui.InputEvent) bool {
	if a.captureAction == "" || ev.Kind != ui.InputKeyDown {
		return false
	}
	if ev.Key == 0 {
		return true // 只按下了修饰键，等真正的主键
	}
	if ev.Key == ui.KeyEscape && ev.Mods == 0 {
		a.captureAction = ""
		a.updateSettingsWindow()
		return true
	}
	a.setChords(a.captureAction, []chord{{Mods: ev.Mods, Key: ev.Key}})
	a.captureAction = ""
	a.updateSettingsWindow()
	return true
}

// breakConflicts 把和新键位冲突的同作用域动作解绑。
func (a *root) breakConflicts(id string, ch chord) {
	act, ok := actionByID(id)
	if !ok {
		return
	}
	for _, other := range keyActions {
		if other.ID == id || other.Scope != act.Scope {
			continue
		}
		var kept []chord
		for _, cs := range a.chords[other.ID] {
			if cs != ch {
				kept = append(kept, cs)
			}
		}
		if len(kept) != len(a.chords[other.ID]) {
			a.chords[other.ID] = kept
		}
	}
}

// keymapHint 是设置窗口里「正在录制」的提示文案。
func keymapHint(a *root) string {
	if a.captureAction == "" {
		return ""
	}
	if act, ok := actionByID(a.captureAction); ok {
		return "按下 " + act.Label + " 的新组合（Esc 取消）"
	}
	return ""
}
