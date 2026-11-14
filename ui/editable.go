package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// editStateKey 标识「当前结果」的可编辑状态；结果变了就重算。
func (a *root) editStateKey(r *engine.QueryResult) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%s|%p|%d", a.currentRuntimeID(), r, len(r.Rows))
}

// discardEditState 丢弃某个运行态的编辑缓存。执行新语句替换结果时调用；
// 钉住的结果有自己的运行态 id，不会被这里动到。
func (a *root) discardEditState(id string) {
	if a.editSets != nil {
		delete(a.editSets, id)
	}
	if id != a.currentRuntimeID() {
		return
	}
	a.editKey = ""
	a.editPlan = nil
	a.readonlyReason = ""
	a.stopInlineEdit()
	a.editCellOpen = false
	a.previewOpen = false
}

// ensureEditState 在结果变化时重算可编辑计划（只读原因也一并算出）。
func (a *root) ensureEditState(r *engine.QueryResult) {
	key := a.editStateKey(r)
	if key == a.editKey {
		return
	}
	a.editKey = key
	if a.editSets == nil {
		a.editSets = map[string]*app.EditSet{}
	}
	a.editPlan = nil
	a.readonlyReason = ""
	if r == nil || !r.IsQuery {
		return
	}
	id := a.currentRuntimeID()
	if a.editSets[id] == nil {
		a.editSets[id] = app.NewEditSet()
	}
	sess := a.activeSession()
	if sess == nil || !sess.Caps().Has(engine.CapEditableResult) {
		a.readonlyReason = "当前数据源不支持写操作"
		return
	}
	if _, ok := sess.(engine.Transactional); !ok {
		a.readonlyReason = "当前数据源不支持事务"
		return
	}
	rels := relationsIn(stmtScopeAt(a.lastExecSQL, 0))
	seen := map[string]string{}
	for _, rel := range rels {
		if rel.Table != "" {
			seen[strings.ToLower(rel.Table)] = rel.Table
		}
	}
	if len(seen) != 1 {
		a.readonlyReason = "只有单表查询可编辑（JOIN/子查询结果只读）"
		return
	}
	for _, rel := range rels {
		if rel.Table != "" && rel.Schema != "" {
			a.readonlyReason = "跨 schema 的查询暂不支持写操作"
			return
		}
	}
	var table string
	for _, name := range seen {
		table = name
	}
	connID := a.activeConsoleConnID()
	scopeKey, _ := a.completionScope(connID)
	cols, all := a.catalog.getColumnsMany(scopeKey, []string{table})
	if !all {
		a.ensureColumns(connID, table)
		// headless（无窗口）下加载是同步的，这里再读一次即可立刻判定。
		cols, all = a.catalog.getColumnsMany(scopeKey, []string{table})
		if !all {
			a.readonlyReason = "正在加载列信息，稍后再试"
			a.editKey = "" // 列加载完成后重算
			return
		}
	}
	infos := make([]app.ColumnInfo, 0, len(cols[0]))
	for _, c := range cols[0] {
		infos = append(infos, app.ColumnInfo{Name: c.Name, Type: c.Type, Nullable: c.Nullable, PrimaryKey: c.PrimaryKey})
	}
	plan, err := app.BuildEditPlan(a.connKind(), table, r.Columns, infos)
	if err != nil {
		a.readonlyReason = err.Error()
		return
	}
	a.editPlan = plan
}

// editSet 返回当前结果的本地改动集。
func (a *root) editSet() *app.EditSet {
	id := a.currentRuntimeID()
	if a.editSets == nil {
		a.editSets = map[string]*app.EditSet{}
	}
	if a.editSets[id] == nil {
		a.editSets[id] = app.NewEditSet()
	}
	return a.editSets[id]
}

// rowsForPlan 返回当前结果的数据行（编辑用的行下标是结果行下标）。
func (a *root) rowsForPlan() [][]any {
	if r := a.currentRuntime().Result(); r != nil {
		return r.Rows
	}
	return nil
}

// setCellEdit 暂存一格改动（结果行下标 + 结果列下标）。
func (a *root) setCellEdit(row, col int, v any) {
	if a.editPlan == nil || row < 0 || col < 0 || col >= len(a.editPlan.Columns) || !a.editPlan.Editable[col] {
		return
	}
	a.editSet().SetCell(row, col, v)
}

// setSelectedNull 把当前选中的所有单元格标成 NULL（本地待提交改动）。
// 主键/只读列与 NOT NULL 列跳过；一格都没改动时在顶栏说明原因。
func (a *root) setSelectedNull(r *engine.QueryResult) {
	if r == nil || !r.IsQuery {
		return
	}
	if a.editPlan == nil {
		if a.readonlyReason != "" {
			a.consoleErr = "只读：" + a.readonlyReason
		}
		return
	}
	rows, cols, _ := a.resultSelection(r)
	if len(rows) == 0 || len(cols) == 0 {
		row := a.displayToResult(r, a.resultRow)
		col := a.cursorDataCol(r)
		if row < 0 || col < 0 {
			return
		}
		rows, cols = []int{row}, []int{col}
	}
	es := a.editSet()
	changed, readonly, nullCol := 0, 0, ""
	for _, row := range rows {
		if row < 0 || row >= len(r.Rows) || es.Deletes[row] {
			continue
		}
		for _, col := range cols {
			if col < 0 || col >= len(a.editPlan.Columns) {
				continue
			}
			if !a.editPlan.Editable[col] {
				readonly++
				continue
			}
			if col >= len(a.editPlan.Nullable) || !a.editPlan.Nullable[col] {
				if nullCol == "" {
					nullCol = a.editPlan.Columns[col]
				}
				continue
			}
			key := app.CellKey{Row: row, Col: col}
			if cur, ok := es.Cells[key]; ok {
				if cur == nil {
					continue // 已经是 NULL
				}
			} else if cellValueOf(r, row, col) == nil {
				continue
			}
			es.SetCell(row, col, nil)
			changed++
		}
	}
	if changed > 0 {
		return
	}
	switch {
	case nullCol != "":
		a.consoleErr = "列 " + nullCol + " 不可为空"
	case readonly > 0:
		a.consoleErr = "主键列不可设为 NULL"
	}
}

// clearRowEdit 还原一行（取消删除标记或丢弃该行改格）。
func (a *root) clearRowEdit(row int) {
	es := a.editSet()
	es.RestoreRow(row)
	for k := range es.Cells {
		if k.Row == row {
			delete(es.Cells, k)
		}
	}
}

// addInsertRow 追加一行待新增。
func (a *root) addInsertRow() int {
	return a.editSet().AddInsert()
}

// deleteRow 标记删除某结果行。
func (a *root) deleteRow(row int) {
	if a.editPlan == nil || row < 0 || row >= len(a.rowsForPlan()) {
		return
	}
	a.editSet().MarkDeleted(row)
}

// previewEdits 生成提交预览（只读 SQL 列表）。
func (a *root) previewEdits() ([]string, error) {
	if a.editPlan == nil {
		return nil, errors.New(a.readonlyReason)
	}
	es := a.editSet()
	if es.Count() == 0 {
		return nil, errors.New("没有待提交的改动")
	}
	return es.Preview(a.editPlan, a.rowsForPlan())
}

// applyEditsSync 在事务里执行全部改动（测试与提交共用；调用方保证不在 UI 线程做长活）。
func (a *root) applyEditsSync() error {
	plan := a.editPlan
	if plan == nil {
		return errors.New(a.readonlyReason)
	}
	es := a.editSet()
	ops, err := es.BuildOps(plan, a.rowsForPlan())
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return errors.New("没有实际改动（值与原来相同或行已被删除）")
	}
	txer, ok := a.activeSession().(engine.Transactional)
	if !ok {
		return errors.New("当前数据源不支持事务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := txer.Begin(ctx)
	if err != nil {
		return err
	}
	if err := app.ApplyOps(ctx, tx, ops); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	es.Clear()
	app.LogInfo("write", "committed", "table", plan.Table, "ops", len(ops))
	return nil
}

// commitEdits 打开预览确认弹窗。
func (a *root) commitEdits() {
	lines, err := a.previewEdits()
	if err != nil {
		a.consoleErr = err.Error()
		return
	}
	a.previewLines = lines
	a.previewOpen = true
}

// setEditStatus 在结果工具条显示一条短状态，3 秒后自动消失。
// 只用于提交成功这类非错误反馈；失败仍走顶栏 consoleErr。
func (a *root) setEditStatus(msg string) {
	a.editStatus = msg
	seq := a.editStatusSeq.Add(1)
	if msg == "" || a.win == nil {
		return
	}
	win := a.win
	time.AfterFunc(3*time.Second, func() {
		if a.editStatusSeq.Load() != seq {
			return
		}
		a.safeUpdate(win, func() {
			if a.editStatusSeq.Load() == seq {
				a.editStatus = ""
			}
		})
	})
}

// applyConfirmedEdits 在确认后执行（有窗口时后台 + Update）。
func (a *root) applyConfirmedEdits() {
	a.previewOpen = false
	if a.editPlan == nil || a.editSet().Count() == 0 {
		return
	}
	a.editBusy = true
	done := func(err error) {
		a.editBusy = false
		if err != nil {
			a.setEditStatus("")
			a.consoleErr = "提交失败（已回滚）：" + err.Error()
			app.LogWarn("write", "commit failed", "err", err.Error())
			return
		}
		a.consoleErr = ""
		a.editKey = ""
		a.refreshResult(func() { a.setEditStatus("提交成功") })
	}
	if a.win == nil {
		done(a.applyEditsSync())
		return
	}
	go func() {
		err := a.applyEditsSync()
		win := a.win
		a.safeUpdate(win, func() { done(err) })
	}()
}

// refreshResult 重跑最近一次语句（刷新结果，保持排序/筛选与分页状态）。
// onDone 在结果回投并更新状态后调用，可为 nil。
func (a *root) refreshResult(onDone func()) {
	sql := a.lastExecSQL
	sess := a.activeSession()
	if sql == "" || sess == nil {
		if onDone != nil {
			onDone()
		}
		return
	}
	id := a.currentRuntimeID()
	rt := a.runtimeFor(id)
	rt.SetOnDone(func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		apply := func() {
			a.recordFetchState(id, res)
			a.editKey = ""
			a.refreshStmtMarks()
			if onDone != nil {
				onDone()
			}
		}
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, apply)
			return
		}
		apply()
	})
	rt.Run(sess, sql)
}

// editCellModal 渲染单元格编辑弹窗；尺寸与查看弹窗一致，长文本用多行编辑器。
func (a *root) editCellModal(c *ui.Context) {
	if !a.editCellOpen {
		return
	}
	w, h := a.cellModalSize()
	ui.Modal(c, &a.editCellOpen, func() {
		ui.Text(c, fmt.Sprintf("编辑 %s · 第 %d 行", a.editPlan.Columns[a.editCellCol], a.editCellRow+1)).Bold()
		ui.TextArea(c, &a.editCellText).AutoFocus().Grow(1)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.editCellOpen = false
			}
			if ui.Button(c, "设为 NULL").Clicked() {
				a.setCellEdit(a.editCellRow, a.editCellCol, nil)
				a.editCellOpen = false
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				a.commitCellEdit(a.editCellRow, a.editCellCol, a.editCellText)
				a.editCellOpen = false
			}
		})
		a.cellModalGrip(c)
	}).Width(w).Height(h)
}

// previewModal 渲染提交预览。
func (a *root) previewModal(c *ui.Context) {
	if !a.previewOpen {
		return
	}
	ui.Modal(c, &a.previewOpen, func() {
		ui.Text(c, fmt.Sprintf("提交 %d 条改动（单事务，影响行数不符会整体回滚）", len(a.previewLines))).Bold()
		ui.ScrollBoth(c).Height(320).Children(func() {
			ui.Text(c, strings.Join(a.previewLines, "\n")).Font("monospace").FontSize(12)
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.previewOpen = false
			}
			if ui.PrimaryButton(c, "确认提交").Clicked() {
				a.applyConfirmedEdits()
			}
		})
	})
}

// parseEditValue 把输入的文本变成值：整串是数字才数字，否则字符串；"NULL" 视为 NULL。
// 必须用 strconv 而不是 fmt.Sscan：Sscan 会把 "2026-10-09 19:17:45" 只解析出开头的 2026。
func parseEditValue(s string) any {
	if s == "NULL" {
		return nil
	}
	t := strings.TrimSpace(s)
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f
	}
	return s
}

// parseEditedValue 把编辑框文本变成值：原本是 NULL 且文本为空时保持 NULL。
func parseEditedValue(original any, s string) any {
	if original == nil && s == "" {
		return nil
	}
	return parseEditValue(s)
}

// editToolbar 渲染写操作按钮；只读时禁用并显示原因。
func (a *root) editToolbar(c *ui.Context, r *engine.QueryResult) {
	es := a.editSet()
	n := es.Count()
	editable := a.editPlan != nil
	if editable {
		if iconButton(c, iconSquarePen, "编辑当前格", ui.Transparent).Clicked() {
			a.beginEditCurrent(r)
		}
		if iconButton(c, iconPlus, "新增行", ui.Transparent).Clicked() {
			a.addInsertRow()
		}
		if iconButton(c, iconMinus, "删除当前行", c.Theme().Danger).Disabled(len(a.rowsForPlan()) == 0).Clicked() {
			a.deleteRow(a.displayToResult(r, a.resultRow))
		}
		if iconButton(c, iconUndo, "撤销改动", ui.Transparent).Disabled(n == 0).Clicked() {
			a.editSet().Clear()
			a.editKey = ""
		}
	}
	if editable {
		label := "提交"
		if n > 0 {
			label = fmt.Sprintf("提交 (%d)", n)
		}
		col := ui.Transparent
		if n > 0 && !a.editBusy {
			col = c.Theme().Accent
		}
		if iconButton(c, iconCheck, label, col).Disabled(n == 0 || a.editBusy).Clicked() {
			a.commitEdits()
		}
	} else if a.readonlyReason != "" {
		ui.Text(c, "只读："+a.readonlyReason).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
	}
}

// inlineEditMaxRunes 是行内编辑的上限；超过或含换行就改用弹窗。
const inlineEditMaxRunes = 60

// inlineEditable 报告内容是否短到适合直接在表格里编辑。
func inlineEditable(text string) bool {
	return !strings.ContainsAny(text, "\r\n") && utf8.RuneCountInString(text) <= inlineEditMaxRunes
}

// editTextFor 是编辑框的预填文本：NULL 显示为空（不能把 "NULL" 当成可编辑内容），
// 其余值沿用结果的显示格式。
func editTextFor(kind string, r *engine.QueryResult, col int, v any) string {
	if v == nil {
		return ""
	}
	return cellStringFor(kind, r, col, v)
}

// beginEditAt 按内容长度选择行内编辑或弹窗编辑。
func (a *root) beginEditAt(r *engine.QueryResult, row, col int) {
	if a.editPlan == nil || col < 0 || col >= len(a.editPlan.Editable) {
		if a.readonlyReason != "" {
			a.consoleErr = "只读：" + a.readonlyReason
		}
		return
	}
	if !a.editPlan.Editable[col] {
		a.consoleErr = "主键列不可编辑"
		return
	}
	if row < 0 || row >= len(r.Rows) {
		return
	}
	kind := a.connKind()
	text := editTextFor(kind, r, col, cellValueOf(r, row, col))
	if pending, ok := a.editSet().Cells[app.CellKey{Row: row, Col: col}]; ok {
		text = editTextFor(kind, r, col, pending)
	}
	if a.inlineEditOn {
		a.stopInlineEdit()
	}
	if inlineEditable(text) {
		a.startInlineEdit(row, col, text)
		return
	}
	a.editCellRow, a.editCellCol, a.editCellText = row, col, text
	a.fitCellModal(text, false)
	a.editCellOpen = true
}

// startInlineEdit 进入表格内联编辑（短内容）。
func (a *root) startInlineEdit(row, col int, text string) {
	a.inlineEditOn, a.inlineEditRow, a.inlineEditCol = true, row, col
	a.inlineEditText, a.inlineEditHadFocus = text, false
}

// stopInlineEdit 退出内联编辑，不提交。
func (a *root) stopInlineEdit() {
	a.inlineEditOn = false
	a.inlineEditRow, a.inlineEditCol = -1, -1
	a.inlineEditText, a.inlineEditHadFocus = "", false
}

// commitCellEdit 把编辑框文本写进待提交改动：
// 与当前值相同就什么都不改（NULL 格打开再保存不会产生空改动）；改回数据库原值则撤掉改动。
func (a *root) commitCellEdit(row, col int, text string) {
	if row < 0 || col < 0 {
		return
	}
	cur := a.editCellValue(row, col)
	next := parseEditedValue(cur, text)
	if app.EqualCell(cur, next) {
		return
	}
	if app.EqualCell(next, cellValueOf(a.currentRuntime().Result(), row, col)) {
		a.editSet().ClearCell(row, col)
		return
	}
	a.setCellEdit(row, col, next)
}

// editCellValue 返回某格当前生效的值（优先本地待提交改动）。
func (a *root) editCellValue(row, col int) any {
	if pending, ok := a.editSet().Cells[app.CellKey{Row: row, Col: col}]; ok {
		return pending
	}
	return cellValueOf(a.currentRuntime().Result(), row, col)
}

// commitInlineEdit 把内联编辑写进待提交改动。
func (a *root) commitInlineEdit() {
	a.commitCellEdit(a.inlineEditRow, a.inlineEditCol, a.inlineEditText)
	a.stopInlineEdit()
}

// inlineCellEditor 在结果表格的单元格里渲染输入框：Enter/失焦提交，Escape 取消。
func (a *root) inlineCellEditor(c *ui.Context) {
	in := ui.TextInput(c, &a.inlineEditText).FontSize(12).Fill().Padding(0, 2)
	if !a.inlineEditHadFocus {
		in.AutoFocus()
	}
	if in.Focused() {
		a.inlineEditHadFocus = true
	}
	switch {
	case in.Submitted():
		a.commitInlineEdit()
	case in.Shortcut(0, ui.KeyEscape):
		a.stopInlineEdit()
	case a.inlineEditHadFocus && !in.Focused():
		a.commitInlineEdit()
	}
}

// beginEditCurrent 打开「编辑格」（当前选中单元格），按内容长度选择行内/弹窗。
func (a *root) beginEditCurrent(r *engine.QueryResult) {
	col := a.cursorDataCol(r)
	row := a.displayToResult(r, a.resultRow)
	a.beginEditAt(r, row, col)
}

func cellValueOf(r *engine.QueryResult, row, col int) any {
	if r == nil || row < 0 || row >= len(r.Rows) || col < 0 || col >= len(r.Rows[row]) {
		return nil
	}
	return r.Rows[row][col]
}
