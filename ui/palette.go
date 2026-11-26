package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/locales"
)

const (
	paletteWidth  = 560
	paletteHeight = 440
	paletteMaxSQL = 30
)

// paletteItem 是命令面板里的一行：label 为主文案，hint 是右侧/下方的补充。
type paletteItem struct {
	label string
	hint  string
	run   func()
}

// openPalette 打开命令面板（标题栏搜索框或 Ctrl+P）。
func (a *root) openPalette() {
	a.paletteQuery = ""
	a.paletteOpen = true
}

// palette 渲染命令面板：搜索框 + 操作 / 控制台 / 控制台内 SQL 三段结果。
func (a *root) palette(c *ui.Context) {
	if !a.paletteOpen {
		return
	}
	t := c.Theme()
	q := strings.ToLower(strings.TrimSpace(a.paletteQuery))
	actions := a.paletteActions(c)
	if q != "" {
		actions = filterPalette(actions, q)
	}
	consoles := a.paletteConsoles()
	if q != "" {
		consoles = filterPalette(consoles, q)
	}
	sql := a.paletteSQL(q)
	var first *paletteItem
	for _, group := range [][]paletteItem{actions, consoles, sql} {
		if len(group) > 0 {
			it := group[0]
			first = &it
			break
		}
	}
	ui.Modal(c, &a.paletteOpen, func() {
		in := ui.TextInput(c, &a.paletteQuery).
			Placeholder(locales.PaletteSearchPlaceholder()).
			Label(locales.PaletteOpen()).AutoFocus()
		ui.Scroll(c).Grow(1).Children(func() {
			if len(actions) == 0 && len(consoles) == 0 && len(sql) == 0 {
				ui.Text(c, locales.PaletteEmpty()).TextColor(t.TextMuted).FontSize(12)
				return
			}
			a.paletteSection(c, locales.PaletteSectionActions(), actions)
			a.paletteSection(c, locales.PaletteSectionConsoles(), consoles)
			a.paletteSection(c, locales.PaletteSectionSQL(), sql)
		})
		if in.Submitted() && first != nil {
			item := *first
			a.paletteOpen = false
			if item.run != nil {
				item.run()
			}
		}
	}).Width(paletteWidth).Height(paletteHeight)
}

// paletteSection 渲染一段标题与它的行。
func (a *root) paletteSection(c *ui.Context, title string, items []paletteItem) {
	if len(items) == 0 {
		return
	}
	t := c.Theme()
	ui.Text(c, title).FontSize(11).Bold().TextColor(t.TextMuted).Padding(t.Space(1), t.Space(2))
	for _, item := range items {
		item := item
		row := ui.ButtonBase(c).Label(item.label).Justify(ui.Start).Gap(8).
			Padding(t.Space(1.25), t.Space(2)).Radius(t.Space(1))
		if row.Hovered() {
			row.Background(t.SurfaceHover)
		}
		row.Children(func() {
			ui.Text(c, item.label).FontSize(12.5).SingleLine()
			if item.hint != "" {
				ui.Text(c, item.hint).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1)
			}
		})
		if row.Clicked() {
			a.paletteOpen = false
			if item.run != nil {
				item.run()
			}
		}
	}
}

// paletteActions 是可搜索的操作：窗口级快捷键动作 + 常用入口。
func (a *root) paletteActions(c *ui.Context) []paletteItem {
	var items []paletteItem
	for _, act := range keyActions {
		if act.ID == "commandPalette" || act.Cmd != myeditor.NoCommand {
			continue
		}
		id := act.ID
		items = append(items, paletteItem{
			label: act.Label(),
			hint:  formatChords(a.chords[id]),
			run:   func() { a.runKeyAction(c, id) },
		})
	}
	sidebarLabel := locales.SidebarHide()
	if a.sidebarHidden {
		sidebarLabel = locales.SidebarShow()
	}
	extras := []paletteItem{
		{label: locales.SidebarNewConn(), run: func() { a.openNewConnForm("") }},
		{label: locales.TopOpenSQLFile(), run: a.pickOpenSQLFiles},
		{label: locales.TopHistory(), run: func() { a.historyOpen = true }},
		{label: locales.TopAIAssistant(), run: a.toggleAIChat},
		{label: locales.SidebarReloadTree(), run: a.reloadTree},
		{label: sidebarLabel, run: func() { a.sidebarHidden = !a.sidebarHidden }},
	}
	return append(items, extras...)
}

// paletteConsoles 是当前工作区打开的控制台，选中即切换。
func (a *root) paletteConsoles() []paletteItem {
	m := a.consoleMgr()
	if m == nil {
		return nil
	}
	var items []paletteItem
	for _, rec := range m.Open() {
		rec := rec
		items = append(items, paletteItem{
			label: a.consoleLabel(rec),
			run:   func() { a.pendingActivate = rec.ID },
		})
	}
	return items
}

// paletteSQL 在打开的控制台正文里找匹配行；一行一项，点击切到那个控制台。
func (a *root) paletteSQL(q string) []paletteItem {
	if q == "" {
		return nil
	}
	m := a.consoleMgr()
	if m == nil {
		return nil
	}
	var items []paletteItem
	for _, rec := range m.Open() {
		text, ok := m.Text(rec.ID)
		if !ok {
			continue
		}
		label := a.consoleLabel(rec)
		id := rec.ID
		lines := strings.Split(text, "\n")
		lineOff := 0
		for i, line := range lines {
			at := strings.Index(strings.ToLower(line), q)
			if at < 0 {
				lineOff += utf8.RuneCountInString(line) + 1 // + 行尾换行
				continue
			}
			snippet := strings.TrimSpace(line)
			if r := []rune(snippet); len(r) > 80 {
				snippet = string(r[:80]) + "…"
			}
			// 跳转时的选区：命中处到查询末尾（rune 偏移）。
			selStart := lineOff + utf8.RuneCountInString(line[:at])
			selEnd := selStart + utf8.RuneCountInString(q)
			items = append(items, paletteItem{
				label: label,
				hint:  fmt.Sprintf("%d: %s", i+1, snippet),
				run: func() {
					a.pendingActivate = id
					a.paletteJumpID = id
					a.paletteJumpSel = [2]int{selStart, selEnd}
				},
			})
			if len(items) >= paletteMaxSQL {
				return items
			}
			lineOff += utf8.RuneCountInString(line) + 1
		}
	}
	return items
}

// applyPaletteJump 在命令面板选中 SQL 命中后把光标/选区放到命中行并滚到可见。
func (a *root) applyPaletteJump() {
	if a.paletteJumpID == "" {
		return
	}
	id := a.paletteJumpID
	a.paletteJumpID = ""
	if a.activeConsole != id {
		return
	}
	a.code.SetSelection(a.paletteJumpSel[0], a.paletteJumpSel[1])
	a.code.state.ScrollTo(a.code.state.CursorPos().Line)
}

// consoleLabel 是面板/标签里控制台的展示名：「数据源 / 控制台」。
func (a *root) consoleLabel(rec app.ConsoleRecord) string {
	src := rec.ConnectionID
	if a.store != nil {
		if p, ok := a.store.Get(rec.ConnectionID); ok {
			src = p.Name
		}
	}
	return src + " / " + rec.Name
}

// filterPalette 按 label 过滤（已转小写的查询）。
func filterPalette(items []paletteItem, q string) []paletteItem {
	out := items[:0]
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}
