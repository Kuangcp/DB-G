package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"db-g/app"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

// displayToResult 把显示顺序的行号换成结果行下标（排序/筛选后两者不同）。
func (a *root) displayToResult(r *engine.QueryResult, displayRow int) int {
	rows := a.resView.apply(r)
	if displayRow < 0 || displayRow >= len(rows) {
		return -1
	}
	return rows[displayRow]
}

// cursorDataCol 把 Cells.Cursor（列 ID）换成结果里的数据列下标；行号列或未知列返回 -1。
func (a *root) cursorDataCol(r *engine.QueryResult) int {
	id := a.resultState.Cells.Cursor
	if id == "" || id == "#" || r == nil {
		return -1
	}
	for i, name := range r.Columns {
		if name == id {
			return i
		}
	}
	return -1
}

// copyCurrentCell 复制当前选中单元格（Ctrl+C 与「复制」按钮共用）。
func (a *root) copyCurrentCell(c *ui.Context, r *engine.QueryResult) {
	col := a.cursorDataCol(r)
	row := a.displayToResult(r, a.resultRow)
	if r == nil || row < 0 || col < 0 || col >= len(r.Columns) {
		return
	}
	c.WriteClipboard(cellString(r.Rows[row][col]))
}

func (a *root) resultPane(c *ui.Context) {
	if c.Shortcut(ui.Ctrl, ui.KeyC) && !a.code.state.Focused() {
		a.copyCurrentCell(c, a.currentRuntime().Result())
	}
	ui.Column(c).Fill().Gap(6).Children(func() {
		if len(a.stmtRuns) > 1 {
			a.stmtTabs(c)
		}
		switch {
		case a.currentRuntime().Running():
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "执行中…")
				if ui.Button(c, "停止").Clicked() {
					a.currentRuntime().Cancel()
				}
			})
		case a.currentRuntime().Err() != "":
			ui.Text(c, "执行失败").Bold()
			ui.Text(c, a.currentRuntime().Err()).TextColor(c.Theme().Danger).Font("monospace")
		case a.currentRuntime().Result() != nil:
			r := a.currentRuntime().Result()
			if !r.IsQuery {
				ui.Textf(c, "OK — %d 行受影响", r.Affected)
				return
			}
			if r.Truncated {
				ui.Badge(c, "结果已截断：超过 500 行或 16MB 字符预算")
			}
			a.resultTable(c, r)
		default:
			switch {
			case a.sess == nil:
				ui.Text(c, "请先连接数据源").TextColor(c.Theme().TextMuted)
			case true:
				ui.Text(c, "Ctrl+Enter 执行光标所在语句（选中则只跑选区）").TextColor(c.Theme().TextMuted)
			default:
				ui.Text(c, "Ctrl+Enter 执行选中 SQL").TextColor(c.Theme().TextMuted)
			}
		}
	})
}

// 结果网格的列宽：按值自适应，带下限与硬上限。
const (
	rowHeaderWidth = 56  // 行号列固定宽
	minColumnWidth = 56  // 每列下限，避免空列挤成缝
	maxColumnWidth = 320 // 每列上限（同时限制手动拖拽）
	charWidth      = 8   // 一个半角字符的估算宽度（DIP）
	cellPad        = 24  // 单元格左右内边距
)

// stmtTabs 是多语句执行的结果 Tab（语句 N · 状态）；点击切换展示。
func (a *root) stmtTabs(c *ui.Context) {
	ui.Row(c).Gap(6).Children(func() {
		for i, run := range a.stmtRuns {
			rt := a.runtimeFor(run.runtimeID)
			label := fmt.Sprintf("语句 %d", i+1)
			switch {
			case !run.started:
				label += " · 未执行"
			case rt.Running():
				label += " · 执行中"
			case rt.Err() != "":
				label += " · 失败"
			default:
				label += " · 成功"
			}
			btn := ui.Button(c, label)
			if i == a.activeStmt {
				btn.Bold()
			}
			if btn.Clicked() {
				a.activeStmt = i
				a.refreshStmtBackgrounds()
			}
		}
	})
}

// dataCols 返回结果数据列的下标（0..n-1）。
func dataCols(r *engine.QueryResult) []int {
	out := make([]int, len(r.Columns))
	for i := range out {
		out[i] = i
	}
	return out
}

// rowTable 返回「复制本行 → INSERT」用的表名：从最近执行语句里取唯一的关系名。
func (a *root) rowTable() (string, bool) {
	rels := relationsIn(a.lastExecSQL)
	seen := map[string]string{}
	for _, r := range rels {
		if r.Table != "" {
			seen[strings.ToLower(r.Table)] = r.Table
		}
	}
	if len(seen) != 1 {
		return "", false
	}
	for _, name := range seen {
		return name, true
	}
	return "", false
}

// connKind 返回当前连接的种类（决定引用符），无连接按 sqlite。
func (a *root) connKind() string {
	if a.sessConnID != "" {
		if p, ok := a.store.Get(a.sessConnID); ok && p.Kind != "" {
			return p.Kind
		}
	}
	return "sqlite"
}

// openCellViewer 打开单元格完整内容弹窗（JSON 美化）。
func (a *root) openCellViewer(r *engine.QueryResult, row, col int) {
	if r == nil || row < 0 || row >= len(r.Rows) || col < 0 || col >= len(r.Columns) {
		return
	}
	text, isJSON := formatCellValue(r.Rows[row][col])
	a.cellViewTitle = r.Columns[col]
	if isJSON {
		a.cellViewTitle += "（JSON）"
	}
	a.cellViewText = text
	a.cellViewOpen = true
}

// cellViewer 渲染长文本查看弹窗。
func (a *root) cellViewer(c *ui.Context) {
	if !a.cellViewOpen {
		return
	}
	ui.Modal(c, &a.cellViewOpen, func() {
		ui.Text(c, a.cellViewTitle).Bold()
		ui.ScrollBoth(c).Height(320).Children(func() {
			ui.Text(c, a.cellViewText).Font("monospace").FontSize(12)
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "复制").Clicked() {
				c.WriteClipboard(a.cellViewText)
			}
			if ui.PrimaryButton(c, "关闭").Clicked() {
				a.cellViewOpen = false
			}
		})
	})
}

func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	widths := a.columnWidths(r)
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: rowHeaderWidth, Fixed: true, Align: ui.End})
	for i, name := range r.Columns {
		cols = append(cols, ui.TableColumn{
			Title:    name,
			ID:       name,
			Width:    widths[i],
			MinWidth: minColumnWidth,
			MaxWidth: maxColumnWidth,
		})
	}
	displayRows := a.resView.apply(r)

	// 顶部操作条：复制/查看/取更多/筛选状态。
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "复制").Clicked() {
			a.copyCurrentCell(c, r)
		}
		if ui.Button(c, "查看").Clicked() {
			a.openCellViewer(r, a.displayToResult(r, a.resultRow), a.cursorDataCol(r))
		}
		if ui.Button(c, "取更多").Disabled(!a.canFetchMore()).Clicked() {
			a.fetchMore()
		}
		ui.MenuButton(c, "导出…", func(m *ui.Menu) {
			if m.Item("当前结果 CSV").Chosen() {
				a.pickExport("csv", false)
			}
			if m.Item("当前结果 JSON").Chosen() {
				a.pickExport("json", false)
			}
			if m.Item("当前结果 SQL INSERT").Chosen() {
				a.pickExport("insert", false)
			}
			full := a.canStream()
			if it := m.Item("全量 CSV（流式，重新执行查询）"); it.Disabled(!full).Chosen() {
				a.confirmFullExport("csv")
			}
			if it := m.Item("全量 JSON（流式，重新执行查询）"); it.Disabled(!full).Chosen() {
				a.confirmFullExport("json")
			}
		})
		if a.exportStatus != "" {
			ui.Text(c, a.exportStatus).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
			if ui.Button(c, "×").Clicked() {
				a.exportStatus = ""
			}
		}
		if n := len(a.resView.filters); n > 0 {
			ui.Text(c, fmt.Sprintf("筛选 %d 列", n)).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		ui.Text(c, "右键表头排序/筛选；右键单元格复制").FontSize(12).TextColor(c.Theme().TextMuted)
	})

	a.resultState.HeaderMenu = func(m *ui.Menu, col int) {
		if col <= 0 {
			return
		}
		idx := col - 1
		if m.Item("升序").Chosen() {
			a.resView.sortBy(r, idx, false)
		}
		if m.Item("降序").Chosen() {
			a.resView.sortBy(r, idx, true)
		}
		if m.Item("清除排序").Chosen() {
			a.resView.sortBy(r, -1, false)
		}
		if m.Item("筛选…").Chosen() {
			a.filterOpen, a.filterCol, a.filterText = true, idx, a.resView.filters[idx]
		}
		if _, has := a.resView.filters[idx]; has {
			if m.Item("清除本列筛选").Chosen() {
				a.resView.clearFilter(idx)
			}
		}
	}

	a.resultState.CellSelect = true
	a.resultState.Selected = &a.resultRow
	a.resultState.CellMenu = func(m *ui.Menu, row, col int) {
		// resultRow 与 Table 一致，存「显示顺序」；取数据时再映射到结果行。
		a.resultRow = row
		row = a.displayToResult(r, row)
		dataCol := col - 1 // 第 0 列是行号
		if dataCol >= 0 && dataCol < len(r.Columns) {
			if m.Item("复制单元格").Chosen() {
				c.WriteClipboard(cellString(r.Rows[row][dataCol]))
			}
		}
		if m.Item("复制本行").Chosen() {
			c.WriteClipboard(rowsTSV(r, []int{row}, dataCols(r), false))
		}
		if it := m.Item("复制本行 → INSERT"); it.Chosen() {
			if table, ok := a.rowTable(); ok {
				c.WriteClipboard(insertSQL(a.connKind(), table, r.Columns, r.Rows[row]))
			}
		} else if table, ok := a.rowTable(); !ok {
			_ = table
		}
		if dataCol >= 0 && m.Item("查看完整内容").Chosen() {
			a.openCellViewer(r, row, dataCol)
		}
	}

	tbl := ui.Table(c, &a.resultState, cols, len(r.Rows), func(row, col int) {
		if row < 0 || row >= len(displayRows) {
			return
		}
		ri := displayRows[row]
		if col == 0 {
			ui.Text(c, strconv.Itoa(ri+1)).TextColor(c.Theme().TextMuted)
			return
		}
		if col-1 >= len(r.Rows[ri]) {
			return
		}
		ui.Text(c, cellText(r.Rows[ri][col-1])).SingleLine()
	}).Grow(1)
	if tbl.Submitted() {
		a.openCellViewer(r, a.displayToResult(r, a.resultRow), a.cursorDataCol(r))
	}
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		// []byte 既可能是 MySQL 的 TEXT/VARCHAR（应渲染文本），也可能是真正的二进制
		// （BLOB）。合法 UTF-8 且无 NUL 时按文本渲染，否则给字节数摘要。
		if isTextBytes(t) {
			return cutRunes(string(t), 512)
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return cutRunes(t, 512)
	default:
		return fmt.Sprint(t)
	}
}

// isTextBytes 判断字节串是否可以当文本显示：合法 UTF-8，且不含 NUL 等控制字符
// （制表/换行除外，它们由表格单行渲染处理）。
func isTextBytes(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == 0 {
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func cutRunes(s string, n int) string {
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if i >= len(s) {
		return s
	}
	return s[:i] + "…"
}

// columnWidths 返回结果各列的宽度（按值自适应，带缓存：同一结果只量一次）。
func (a *root) columnWidths(r *engine.QueryResult) []float32 {
	if a.widthsFor == r && len(a.widths) == len(r.Columns) {
		return a.widths
	}
	a.widthsFor = r
	a.widths = resultWidths(r)
	return a.widths
}

// resultWidths 按表头与每一行渲染后的文本量出各列宽度。
func resultWidths(r *engine.QueryResult) []float32 {
	out := make([]float32, len(r.Columns))
	for i := range r.Columns {
		units := displayWidth(r.Columns[i])
		for _, row := range r.Rows {
			if i >= len(row) {
				continue
			}
			if u := displayWidth(cellText(row[i])); u > units {
				units = u
			}
		}
		w := float32(units*charWidth + cellPad)
		out[i] = min(max(w, minColumnWidth), maxColumnWidth)
	}
	return out
}

// displayWidth 估算字符串的显示宽度：半角 1 个单位，中日韩/全角 2 个单位。
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == 0 || r == '\n' || r == '\r' || r == '\t':
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x1100 && (r <= 0x115f || // Hangul Jamo
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || // CJK 部首/假名/汉字
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul 音节
		(r >= 0xf900 && r <= 0xfaff) || // CJK 兼容汉字
		(r >= 0xfe30 && r <= 0xfe6f) || // CJK 兼容形式
		(r >= 0xff00 && r <= 0xff60) || // 全角 ASCII
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f9ff) || // emoji
		(r >= 0x20000 && r <= 0x3fffd)): // CJK 扩展
		return 2
	}
	return 1
}

// canFetchMore 报告当前结果能否「取更多」：单条查询、驱动支持、还有更多行。
func (a *root) canFetchMore() bool {
	rt := a.currentRuntime()
	r := rt.Result()
	if r == nil || !r.IsQuery || rt.Running() || a.sess == nil || !a.sess.Caps().Has(engine.CapFetchMore) {
		return false
	}
	if _, ok := fetchMoreSQL(a.lastExecSQL, 0, 1); !ok {
		return false
	}
	st, ok := a.fetchStates[a.currentRuntimeID()]
	return ok && st.more
}

// fetchMore 用派生表分页把后续行追加到当前结果（不重跑原 SQL 的第一页）。
func (a *root) fetchMore() {
	if !a.canFetchMore() {
		return
	}
	id := a.currentRuntimeID()
	st := a.fetchStates[id]
	sql, ok := fetchMoreSQL(a.lastExecSQL, st.loaded, 500)
	if !ok {
		a.consoleErr = "取更多失败：当前语句不支持分页"
		return
	}
	base := a.currentRuntime()
	more := a.runtimeFor(id + "#more")
	more.SetOnDone(func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		apply := func() {
			if errMsg != "" {
				a.consoleErr = "取更多失败: " + errMsg
				return
			}
			if res == nil || base.Result() == nil {
				return
			}
			r := base.Result()
			r.Rows = append(r.Rows, res.Rows...)
			r.Truncated = res.Truncated
			a.fetchStates[id] = fetchState{loaded: len(r.Rows), more: res.Truncated && res.IsQuery}
			a.resView.key = ""
		}
		if a.win != nil {
			win := a.win
			win.Update(apply)
			return
		}
		apply()
	})
	if !more.Run(a.sess, sql) {
		a.consoleErr = "取更多失败：无法启动"
	}
}

// filterModal 渲染每列筛选输入。
func (a *root) filterModal(c *ui.Context) {
	if !a.filterOpen {
		return
	}
	ui.Modal(c, &a.filterOpen, func() {
		title := "筛选"
		if r := a.currentRuntime().Result(); r != nil && a.filterCol >= 0 && a.filterCol < len(r.Columns) {
			title = "筛选：" + r.Columns[a.filterCol]
		}
		ui.Text(c, title).Bold()
		ui.TextInput(c, &a.filterText).Placeholder("包含…（留空清除）").AutoFocus()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.filterOpen = false
			}
			if ui.PrimaryButton(c, "应用").Clicked() {
				a.resView.setFilter(a.filterCol, a.filterText)
				a.filterOpen = false
			}
		})
	})
}

// recordFetchState 在语句执行成功后记录分页状态。
func (a *root) recordFetchState(runtimeID string, res *engine.QueryResult) {
	if res == nil || !res.IsQuery {
		delete(a.fetchStates, runtimeID)
		return
	}
	a.fetchStates[runtimeID] = fetchState{loaded: len(res.Rows), more: res.Truncated}
	a.resView.key = ""
}

// canStream 报告能否全量流式导出（驱动实现 RowStreamer 且有可重放的 SQL）。
func (a *root) canStream() bool {
	if a.sess == nil || strings.TrimSpace(a.lastExecSQL) == "" {
		return false
	}
	_, ok := a.sess.(engine.RowStreamer)
	return ok
}

// confirmFullExport 打开「会重新执行查询」的确认弹窗。
func (a *root) confirmFullExport(format string) {
	a.exportFormat = format
	a.exportConfirm = true
}

// exportConfirmModal 渲染全量导出确认。
func (a *root) exportConfirmModal(c *ui.Context) {
	if !a.exportConfirm {
		return
	}
	ui.Modal(c, &a.exportConfirm, func() {
		ui.Text(c, "全量导出（"+a.exportFormat+"）").Bold()
		ui.Text(c, "会重新执行当前语句读取全部结果，数据可能与屏幕上略有差异。").TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.exportConfirm = false
			}
			if ui.PrimaryButton(c, "选择文件…").Clicked() {
				format := a.exportFormat
				a.exportConfirm = false
				a.pickExport(format, true)
			}
		})
	})
}

// pickExport 弹保存对话框（goroutine），选定后后台导出。
func (a *root) pickExport(format string, full bool) {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		ext := "." + format
		if format == "insert" {
			ext = ".sql"
		}
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "导出结果",
			DefaultPath: "result" + ext,
		})
		if err != nil || path == "" {
			return
		}
		win.Update(func() {
			a.exportStatus = "导出中…"
		})
		n, err := a.exportTo(path, format, full)
		win.Update(func() {
			if err != nil {
				a.exportStatus = "导出失败: " + err.Error()
				app.LogError("export", "failed", "path", path, "format", format, "err", err.Error())
				return
			}
			a.exportStatus = fmt.Sprintf("已导出 %d 行 → %s", n, path)
			app.LogInfo("export", "done", "path", path, "format", format, "rows", n, "full", full)
		})
	}()
}

// exportTo 同步导出（对话框之外的实现；测试直接调用）。
func (a *root) exportTo(path, format string, full bool) (int, error) {
	kind := a.connKind()
	table := ""
	if _, ok := fetchMoreSQL(a.lastExecSQL, 0, 1); ok {
		if t, ok := a.rowTable(); ok {
			table = t
		}
	}
	if format == "insert" && table == "" {
		return 0, errors.New("无法确定表名（多表查询），不能导出 INSERT")
	}
	if full {
		streamer, ok := a.sess.(engine.RowStreamer)
		if !ok {
			return 0, errors.New("当前数据源不支持流式导出")
		}
		sql := a.lastExecSQL
		stream := func(onCols func([]string) error, onRow func([]any) error) error {
			return streamer.Stream(context.Background(), sql, onCols, onRow)
		}
		return app.ExportStream(path, format, kind, table, stream)
	}
	r := a.currentRuntime().Result()
	if r == nil || !r.IsQuery {
		return 0, errors.New("没有可导出的查询结果")
	}
	rows := make([][]any, len(r.Rows))
	copy(rows, r.Rows)
	return app.ExportRows(path, format, kind, table, r.Columns, rows)
}
