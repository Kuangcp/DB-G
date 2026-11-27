package ui

import (
	"bytes"
	"context"
	"encoding/csv"
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
	"db-g/locales"
	"db-g/sql"
)

// displayToResult 把显示顺序的行号换成结果行下标（排序/筛选后两者不同）。
func (a *root) displayToResult(r *engine.QueryResult, displayRow int) int {
	rows := a.grid.view.apply(r)
	if displayRow < 0 || displayRow >= len(rows) {
		return -1
	}
	return rows[displayRow]
}

// cursorDataCol 把 Cells.Cursor（列 ID）换成结果里的数据列下标；行号列或未知列返回 -1。
func (a *root) cursorDataCol(r *engine.QueryResult) int {
	id := a.grid.state.Cells.Cursor
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

// copyCurrentCell 复制当前选区（多格时按 TSV）或光标所在单元格（Ctrl+C 与「复制」按钮共用）。
func (a *root) copyCurrentCell(c *ui.Context, r *engine.QueryResult) {
	kind := a.gridKind()
	if rows, cols, multi := a.resultSelection(r); multi {
		c.WriteClipboard(rowsTSVFor(kind, r, rows, cols, false))
		return
	}
	col := a.cursorDataCol(r)
	row := a.displayToResult(r, a.grid.row)
	if r == nil || row < 0 || col < 0 || col >= len(r.Columns) {
		return
	}
	c.WriteClipboard(cellStringFor(kind, r, col, r.Rows[row][col]))
}

// resultSelection 返回当前单元格块选中的结果行与数据列下标（都按显示顺序），
// 以及块是否大于 1×1。行号列（#）不算数据列。
func (a *root) resultSelection(r *engine.QueryResult) (rows, cols []int, multi bool) {
	if r == nil {
		return nil, nil, false
	}
	chosen := make(map[int]bool)
	for k := range a.grid.sel.All() {
		chosen[k] = true
	}
	for i, ri := range a.grid.view.apply(r) {
		if chosen[i] {
			rows = append(rows, ri)
		}
	}
	for _, id := range a.grid.state.SelectedColumns() {
		if id == "#" {
			continue
		}
		for j, name := range r.Columns {
			if name == id {
				cols = append(cols, j)
				break
			}
		}
	}
	return rows, cols, len(rows) > 1 || len(cols) > 1
}

// blockCSV 把选区写成 CSV（与导出同一套转义；二进制给 base64 摘要）。
func blockCSV(kind string, r *engine.QueryResult, rows, cols []int, header bool) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if header {
		names := make([]string, len(cols))
		for i, col := range cols {
			names[i] = r.Columns[col]
		}
		_ = w.Write(names)
	}
	for _, row := range rows {
		rec := make([]string, len(cols))
		for i, col := range cols {
			rec[i] = cellStringFor(kind, r, col, cellAt(r.Rows[row], col))
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

func (a *root) resultPane(c *ui.Context) {
	if a.ai.Plan.open {
		a.planPane(c)
		return
	}
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
			// 多语句或存在钉住结果时显示结果 tab：pinned 在左，新结果在右。
			if len(a.tabs.StmtRuns) > 1 || a.hasPinned() {
				a.stmtTabs(c)
			}
			switch {
			case a.currentRuntime().Running():
				rt := a.currentRuntime()
				rows, bytes := rt.Progress()
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					status := locales.ResultRunning()
					if rt.Canceling() {
						status = locales.ResultCanceling()
					}
					ui.Text(c, status)
					if rows > 0 {
						detail := locales.ResultReceivedRows(rows)
						if bytes > 0 {
							detail += " · " + humanBytes(bytes)
						}
						ui.Text(c, detail).FontSize(12).TextColor(c.Theme().TextMuted)
					}
					if !rt.Canceling() && ui.Button(c, locales.ResultStop()).Clicked() {
						a.stopActiveRuns()
					}
				})
			case a.currentRuntime().Err() != "":
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, locales.ResultFailed()).Bold()
					ui.Spacer(c)
					if ui.Button(c, locales.ResultAIFix()).Clicked() {
						a.fixErrorSQL()
					}
				})
				a.withResultFont(ui.Text(c, a.currentRuntime().Err()).TextColor(c.Theme().Danger).Font("monospace"))
			case a.currentRuntime().Result() != nil:
				r := a.currentRuntime().Result()
				if !r.IsQuery {
					ui.Text(c, locales.ResultAffected(int(r.Affected)))
					return
				}
				if r.Truncated {
					ui.Badge(c, locales.ResultTruncated(a.settings.ResultPrefs.BudgetMB))
				}
				a.resultTable(c, r)
			default:
				switch {
				case a.activeSession() == nil:
					ui.Text(c, locales.ResultConnectFirst()).TextColor(c.Theme().TextMuted)
				case true:
					ui.Text(c, locales.ResultRunHint()).TextColor(c.Theme().TextMuted)
				default:
					ui.Text(c, locales.ResultRunSelectionHint()).TextColor(c.Theme().TextMuted)
				}
			}
		})
	})
}

// 结果网格的列宽：按值自适应，带下限与硬上限。
const (
	rowHeaderWidth = 56  // 行号列固定宽
	minColumnWidth = 56  // 每列下限，避免空列挤成缝
	maxColumnWidth = 320 // 自动测量的初始宽度上限；手动拖拽不受此限制
	charWidth      = 8   // 一个半角字符的估算宽度（DIP）
	cellPad        = 24  // 单元格左右内边距
)

// stmtTabs 是结果区的 Tab 条（结果 N · 状态）：pinned 固定在最左，新执行追加到右侧。
// 点击切换当前展示的结果；钉住的结果带图钉标记（对照 db-k 的 ResultChip）。
func (a *root) stmtTabs(c *ui.Context) {
	t := c.Theme()
	ui.ScrollHorizontal(c).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for i := range a.tabs.StmtRuns {
				run := a.tabs.StmtRuns[i]
				rt := a.runtimeFor(run.runtimeID)
				active := i == a.tabs.ActiveStmt
				label := locales.ResultTab(run.tabNo)
				state := locales.ResultStateSuccess()
				switch {
				case !run.started:
					state = locales.ResultStateNotRun()
				case rt.Running():
					state = locales.ResultStateRunning()
				case rt.Err() != "":
					state = locales.ResultStateFailed()
				}
				btn := ui.ButtonBase(c).Label(label+" · "+state).Gap(4).
					Padding(2, 7).Radius(t.Space(1.25))
				if active {
					btn.Background(t.Accent.Alpha(0.14))
				} else if btn.Hovered() {
					btn.Background(t.SurfaceHover)
				}
				btn.Children(func() {
					if run.pinned {
						ui.Icon(c, iconPin).FontSize(12).TextColor(t.Accent)
					}
					txt := ui.Text(c, label).FontSize(12)
					switch {
					case active:
						txt.Bold().TextColor(t.Accent)
					case rt.Err() != "":
						txt.TextColor(t.Danger)
					default:
						txt.TextColor(t.TextMuted)
					}
					// 结束时用状态点代替文案：绿=成功，红=失败；未执行/执行中仍显示文字。
					switch state {
					case locales.ResultStateNotRun(), locales.ResultStateRunning():
						ui.Text(c, "· "+state).FontSize(11).TextColor(t.TextMuted)
					case locales.ResultStateFailed():
						dot := ui.Text(c, "●").FontSize(10).TextColor(t.Danger)
						if err := rt.Err(); err != "" {
							dot.Tooltip(err)
						}
					default:
						ui.Text(c, "●").FontSize(10).TextColor(t.Success)
					}
				})
				if btn.Clicked() {
					a.tabs.ActiveStmt = i
					a.refreshStmtMarks()
				}
			}
		})
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
	rels := sql.Relations(a.gridSQL())
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
	if k := a.connKindOf(a.activeConnID()); k != "" {
		return k
	}
	return "sqlite"
}

// openCellViewer 打开单元格完整内容弹窗：合法 JSON 对象/数组渲染成可折叠树，
// 其它内容按原文本显示。
func (a *root) openCellViewer(r *engine.QueryResult, row, col int) {
	if r == nil || row < 0 || row >= len(r.Rows) || col < 0 || col >= len(r.Columns) {
		return
	}
	v := r.Rows[row][col]
	if pending, ok := a.editSet().Cells[app.CellKey{Row: row, Col: col}]; ok {
		v = pending
	}
	text, isJSON := formatCellText(cellStringFor(a.gridKind(), r, col, v))
	a.cellView.Title = r.Columns[col]
	if isJSON {
		a.cellView.Title += "（JSON）"
	}
	a.cellView.Text = text
	a.fitCellModal(text, isJSON)
	a.cellView.JSONOK = false
	a.cellView.JSONRoot, a.cellView.JSONRows = nil, nil
	a.cellView.JSONExpanded, a.cellView.JSONTruncated = nil, false
	a.cellView.JSONList = ui.ListState{}
	a.cellView.FindOpen = false
	a.cellView.FindQuery, a.cellView.FindLast = "", ""
	a.cellView.FindHits, a.cellView.FindCur = nil, 0
	// 避免对超大文本做树解析（解析与节点量都随大小爆炸），超出就退回纯文本。
	if isJSON && len(a.cellView.Text) <= 1<<20 {
		if root, truncated, err := buildJSONTree([]byte(a.cellView.Text)); err == nil &&
			(root.kind == "object" || root.kind == "array") {
			a.applyCellJSON(root, truncated)
			a.cellView.JSONOK = true
		}
	}
	a.cellView.Open = true
}

// humanBytes 把字节数缩成 KB/MB，结果区的进度提示用。
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// 单元格查看/编辑弹窗的默认与最小尺寸；窗口再小时由 Modal 的 MaxWidth/MaxHeight 兜底。
const (
	cellModalDefaultW = 880
	cellModalDefaultH = 660
	cellModalMinW     = 360
	cellModalMinH     = 240
)

// cellModalSize 返回查看/编辑弹窗当前尺寸；首次使用时补默认值。
func (a *root) cellModalSize() (float32, float32) {
	if a.cellView.ModalW <= 0 {
		a.cellView.ModalW = cellModalDefaultW
	}
	if a.cellView.ModalH <= 0 {
		a.cellView.ModalH = cellModalDefaultH
	}
	return a.cellView.ModalW, a.cellView.ModalH
}

// fitCellModal 按内容给查看/编辑弹窗一个合适的初始尺寸：短文本用小窗口，
// 大段文本/复杂 JSON 才用默认大窗口；用户仍可拖拽右下角调整。
func (a *root) fitCellModal(text string, isJSON bool) {
	const charW = 7.5
	minW, minH := float32(380), float32(220)
	if isJSON {
		minW, minH = 520, 380
	}
	lines := strings.Split(text, "\n")
	maxRunes, total := 0, 0
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		if n > maxRunes {
			maxRunes = n
		}
		total += n
	}
	if total == 0 {
		a.cellView.ModalW, a.cellView.ModalH = minW, minH
		return
	}
	w := min(max(float32(maxRunes)*charW+100, minW), cellModalDefaultW)
	perLine := max(int((w-60)/charW), 8)
	wrapped := 0
	for _, line := range lines {
		n := utf8.RuneCountInString(line)
		wrapped += max((n+perLine-1)/perLine, 1)
	}
	h := min(max(float32(wrapped)*20+110, minH), cellModalDefaultH)
	a.cellView.ModalW, a.cellView.ModalH = w, h
}

// cellModalGrip 是弹窗右下角的拖拽调整大小手柄。面板在窗口里居中，
// 所以右下角要跟住指针，宽高按两倍增量变化。
func (a *root) cellModalGrip(c *ui.Context) {
	a.cellModalSize()
	grip := ui.Box(c).Absolute().Right(2).Bottom(2).Size(18, 18).Cursor(ui.CursorResizeNWSE).
		Label(locales.CellViewerResize()).Tooltip(locales.CellViewerResizeTip())
	col := c.Theme().TextMuted
	if grip.Hovered() || grip.Pressed() {
		col = c.Theme().Accent
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		var path ui.Path
		path.MoveTo(r.X+r.W-4, r.Y+r.H-11).LineTo(r.X+r.W-11, r.Y+r.H-4)
		path.MoveTo(r.X+r.W-4, r.Y+r.H-6).LineTo(r.X+r.W-6, r.Y+r.H-4)
		p.StrokePath(&path, 1.5, col)
	})
	if dx, dy, held := grip.Dragged(); held {
		w, h := c.Size()
		a.cellView.ModalW = min(max(a.cellView.ModalW+2*dx, cellModalMinW), w-40)
		a.cellView.ModalH = min(max(a.cellView.ModalH+2*dy, cellModalMinH), h-40)
	}
}

// viewerToolbar 是单元格查看器的标题栏：标题在左，JSON 折叠控制与复制/关闭在右。
func (a *root) viewerToolbar(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		ui.Text(c, a.cellView.Title).Bold().SingleLine()
		if a.cellView.JSONOK && a.cellView.JSONTruncated {
			ui.Text(c, locales.JsonTooManyNodes(maxJSONTreeRows)).
				FontSize(11).TextColor(c.Theme().Warning).SingleLine()
		}
		ui.Spacer(c)
		if a.cellView.JSONOK {
			if iconButtonPlain(c, iconSearch, locales.CellViewerFind(), ui.Transparent).Clicked() {
				a.cellView.FindOpen = !a.cellView.FindOpen
				if a.cellView.FindOpen {
					a.refreshCellFind()
				}
			}
			if iconButtonPlain(c, iconCollapseAll, locales.JsonCollapseAll(), ui.Transparent).Clicked() {
				a.expandAllJSON(false)
			}
			if iconButtonPlain(c, iconExpandAll, locales.JsonExpandAll(), ui.Transparent).Clicked() {
				a.expandAllJSON(true)
			}
		} else {
			label := locales.JsonWrapOn()
			col := c.Theme().Accent
			if !a.cellView.Wrap {
				label, col = locales.JsonWrapOff(), ui.Transparent
			}
			if iconButtonPlain(c, iconWrapText, label, col).Clicked() {
				a.cellView.Wrap = !a.cellView.Wrap
			}
		}
		if iconButtonPlain(c, iconCopy, locales.JsonCopyRaw(), ui.Transparent).Tooltip(locales.JsonCopyRawTip()).Clicked() {
			c.WriteClipboard(a.cellView.Text)
		}
		if iconButtonPlain(c, iconClose, locales.CommonClose(), ui.Transparent).Clicked() {
			a.cellView.Open = false
		}
	})
}

// cellViewer 渲染长文本查看弹窗；JSON 时画可展开/折叠的树。
func (a *root) cellViewer(c *ui.Context) {
	if !a.cellView.Open {
		return
	}
	w, h := a.cellModalSize()
	m := ui.Modal(c, &a.cellView.Open, func() {
		a.viewerToolbar(c)
		if a.cellView.JSONOK && a.cellView.FindOpen {
			a.cellViewFindBar(c)
		}
		switch {
		case a.cellView.JSONOK:
			a.jsonRowsView(c)
		case a.cellView.Wrap:
			// 纵向滚动容器里再套一层 FillWidth，文本才会拿到视口宽度并折行。
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					a.withResultFont(ui.Text(c, a.cellView.Text).Font("monospace").FontSize(12))
				})
			})
		default:
			// 不折行：整行保留自然宽度，交给横向滚动查看。
			ui.ScrollBoth(c).Grow(1).Children(func() {
				a.withResultFont(ui.Text(c, a.cellView.Text).Font("monospace").FontSize(12).NoWrap())
			})
		}
		a.cellModalGrip(c)
	}).Width(w).Height(h)
	if a.cellView.JSONOK && m.Shortcut(ui.Ctrl, ui.KeyF) {
		a.cellView.FindOpen = true
		a.refreshCellFind()
	}
}

const (
	gridConsole = iota
	gridViewer
)

// resultTable 渲染控制台的结果网格：与表视图数据页共用 gridTable。
func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	a.gridTable(c, r, gridConsole)
}

// gridTable 渲染一块结果网格（只读/可编辑由编辑计划决定）。
// mode 区分控制台结果区与表视图数据页：钉住、导出、本地排序筛选只给控制台。
func (a *root) gridTable(c *ui.Context, r *engine.QueryResult, mode int) {
	widths := a.columnWidths(r)
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: rowHeaderWidth, Fixed: true, Align: ui.End})
	for i, name := range r.Columns {
		cols = append(cols, ui.TableColumn{
			Title:    name,
			ID:       name,
			Width:    widths[i],
			MinWidth: minColumnWidth,
			// 不设 MaxWidth：初始宽度按内容封顶，但用户拖拽可以继续拉宽。
		})
	}
	a.ensureEditState(r)
	displayRows := a.grid.view.apply(r)
	editSet := a.editSet()
	insertRows := len(editSet.Inserts)
	totalRows := len(displayRows) + insertRows

	// 顶部操作条：刷新/取更多/钉住/编辑。工具条里的按钮悬停前没有面孔。
	if mode == gridConsole {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Toolbar(c, func() {
				if sql, ok := a.rerunnableSQL(); ok {
					if iconButton(c, iconRefresh, locales.ResultRerun(), ui.Transparent).
						Disabled(a.anyRunRunning() || a.activeSession() == nil).Clicked() {
						a.rerunQuery(sql)
					}
				}
				a.pageToolbar(c, r)
				a.editToolbar(c, r)
			}).Label(locales.ResultActions())
			ui.Divider(c)
			a.pinButton(c)
			iconButtonPlain(c, iconDownload, locales.ExportMenu(), ui.Transparent).Disabled(a.exportState.Busy).Menu(func(m *ui.Menu) {
				if m.Item(locales.ExportCurrentCSV()).Chosen() {
					a.pickExport("csv", false)
				}
				if m.Item(locales.ExportCurrentJSON()).Chosen() {
					a.pickExport("json", false)
				}
				if m.Item(locales.ExportCurrentJSONL()).Chosen() {
					a.pickExport("jsonl", false)
				}
				if m.Item(locales.ExportCurrentInsert()).Chosen() {
					a.pickExport("insert", false)
				}
				m.Separator()
				full := a.canStream()
				if it := m.Item(locales.ExportFullCSV()); it.Disabled(!full).Chosen() {
					a.confirmFullExport("csv")
				}
				if it := m.Item(locales.ExportFullJSON()); it.Disabled(!full).Chosen() {
					a.confirmFullExport("json")
				}
				if it := m.Item(locales.ExportFullJSONL()); it.Disabled(!full).Chosen() {
					a.confirmFullExport("jsonl")
				}
			})
			if a.exportState.Busy {
				ui.Spinner(c)
				label := locales.ExportRunning(strings.ToUpper(a.exportState.Format))
				if a.exportState.Full {
					label = locales.ExportFullRunning(strings.ToUpper(a.exportState.Format))
				}
				if n := a.exportState.Rows.Load(); n > 0 {
					label += locales.ExportWritten(int(n))
				}
				ui.Text(c, label).FontSize(12).TextColor(c.Theme().Accent).SingleLine()
			} else if a.exportState.Status != "" {
				status := ui.Text(c, a.exportState.Status).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
				if a.exportState.Path != "" {
					status.Tooltip(locales.ExportOpenFolderTip())
					if status.DoubleClicked() {
						a.openExportFolder()
					}
				}
				if iconButtonPlain(c, iconClose, locales.ExportClearTip(), ui.Transparent).Clicked() {
					a.exportState.Status = ""
					a.exportState.Path = ""
				}
			}
			if a.edit.Status != "" {
				ui.Text(c, a.edit.Status).FontSize(12).TextColor(c.Theme().Success).SingleLine()
			}
			if n := len(a.grid.view.filters); n > 0 {
				ui.Text(c, locales.FilterColumns(n)).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
	}

	a.grid.state.HeaderMenu = nil
	if mode == gridConsole {
		a.grid.state.HeaderMenu = func(m *ui.Menu, col int) {
			if col <= 0 {
				return
			}
			idx := col - 1
			if idx < len(r.Columns) && m.Item(locales.CopyColumnName()).Chosen() {
				c.WriteClipboard(r.Columns[idx])
			}
			if m.Item(locales.SortAsc()).Chosen() {
				a.grid.view.sortBy(r, idx, false)
			}
			if m.Item(locales.SortDesc()).Chosen() {
				a.grid.view.sortBy(r, idx, true)
			}
			if m.Item(locales.SortClear()).Chosen() {
				a.grid.view.sortBy(r, -1, false)
			}
			if m.Item(locales.FilterMenu()).Chosen() {
				a.grid.filterOpen, a.grid.filterCol, a.grid.filterText = true, idx, a.grid.view.filters[idx]
			}
			if _, has := a.grid.view.filters[idx]; has {
				if m.Item(locales.FilterClearColumn()).Chosen() {
					a.grid.view.clearFilter(idx)
				}
			}
		}
	}

	// 单元格级选择：Selection 与 Selected 同时设置才真正进入 CellSelect
	// （mygo 要求两者都有），点击会同时移动行与列光标。
	if a.grid.last != r {
		a.grid.last = r
		a.grid.view.clear()
		a.grid.sel.Clear()
		a.stopInlineEdit()
		// 列光标默认落在第一列数据（不是行号列），未点击时的复制/查看也有目标。
		a.grid.state.Cells = ui.ColumnRange{}
		if len(r.Columns) > 0 {
			a.grid.state.Cells = ui.ColumnRange{Cursor: r.Columns[0], Anchor: r.Columns[0]}
		}
	}
	a.grid.state.Selection = &a.grid.sel
	a.grid.state.CellSelect = true
	a.grid.state.Selected = &a.grid.row
	a.grid.state.CellsStyle = ui.CellsStyle{
		Cell:     c.Theme().Accent,
		CellText: c.Theme().AccentText,
		Row:      c.Theme().Accent.Alpha(0.12),
		Stripe:   c.Theme().Text.Alpha(0.035),
	}
	kind := a.gridKind()
	a.grid.state.CellMenu = func(m *ui.Menu, row, col int) {
		// resultRow 与 Table 一致，存「显示顺序」；取数据时再映射到结果行。
		displayRow := row
		a.grid.row = displayRow
		dataRows := len(a.grid.view.apply(r))
		dataCol := col - 1 // 第 0 列是行号
		if displayRow >= dataRows {
			insIdx := displayRow - dataRows
			if a.grid.plan != nil && dataCol >= 0 && dataCol < len(a.grid.plan.Columns) && dataCol < len(a.grid.plan.Nullable) {
				if m.Item(locales.EditCellMenu()).Chosen() {
					a.beginEditAt(r, -1-insIdx, dataCol)
				}
				if a.grid.plan.Nullable[dataCol] && m.Item(locales.SetNull()).Chosen() {
					a.setCellEdit(-1-insIdx, dataCol, nil)
				}
				if _, ok := a.editSet().Inserts[insIdx][dataCol]; ok && m.Item(locales.RestoreCell()).Chosen() {
					a.editSet().ClearInsertCell(insIdx, dataCol)
				}
			}
			if m.Item(locales.DeleteInsertedRow()).Chosen() {
				a.removeInsertRow(r, insIdx)
			}
			return
		}
		row = a.displayToResult(r, displayRow)
		if row < 0 {
			return
		}
		if dataCol >= 0 && dataCol < len(r.Columns) {
			if m.Item(locales.CopyCell()).Chosen() {
				c.WriteClipboard(cellStringFor(kind, r, dataCol, r.Rows[row][dataCol]))
			}
		}
		if m.Item(locales.CopyRow()).Chosen() {
			c.WriteClipboard(rowsTSVFor(kind, r, []int{row}, dataCols(r), false))
		}
		if it := m.Item(locales.CopyRowInsert()); it.Chosen() {
			if table, ok := a.rowTable(); ok {
				c.WriteClipboard(insertSQLFor(kind, table, r.Columns, r.ColumnTypes, r.Rows[row]))
			}
		} else if table, ok := a.rowTable(); !ok {
			_ = table
		}
		if rows, cols, multi := a.resultSelection(r); multi {
			m.Submenu(locales.CopySelection(), func(sm *ui.Menu) {
				if sm.Item(locales.CopySelectionTSV()).Chosen() {
					c.WriteClipboard(rowsTSVFor(kind, r, rows, cols, false))
				}
				if sm.Item(locales.CopySelectionTSVHeader()).Chosen() {
					c.WriteClipboard(rowsTSVFor(kind, r, rows, cols, true))
				}
				if sm.Item(locales.CopySelectionCSV()).Chosen() {
					c.WriteClipboard(blockCSV(kind, r, rows, cols, false))
				}
			})
			_, hasTable := a.rowTable()
			if mode == gridConsole {
				m.Submenu(locales.ExportSelection(), func(sm *ui.Menu) {
					if sm.Item("CSV…").Chosen() {
						a.pickExportSelection("csv", rows, cols)
					}
					if sm.Item("JSON…").Chosen() {
						a.pickExportSelection("json", rows, cols)
					}
					if sm.Item("JSONL…").Chosen() {
						a.pickExportSelection("jsonl", rows, cols)
					}
					if it := sm.Item("SQL INSERT…"); it.Disabled(!hasTable).Chosen() {
						a.pickExportSelection("insert", rows, cols)
					}
				})
			}
		}
		if dataCol >= 0 && m.Item(locales.ViewFullContent()).Chosen() {
			a.openCellViewer(r, row, dataCol)
		}
		if a.grid.plan != nil && dataCol >= 0 {
			if it := m.Item(locales.EditCellMenu()); it.Disabled(!a.grid.plan.Editable[dataCol]).Chosen() {
				a.edit.CellRow, a.edit.CellCol = row, dataCol
				a.edit.CellText = editTextFor(kind, r, dataCol, a.editCellValue(row, dataCol))
				a.edit.CellOpen = true
			}
			if a.grid.plan.Editable[dataCol] && m.Item(locales.SetNull()).Chosen() {
				a.setCellEdit(row, dataCol, nil)
			}
			if _, has := a.editSet().Cells[app.CellKey{Row: row, Col: dataCol}]; has {
				if m.Item(locales.RestoreCell()).Chosen() {
					a.editSet().ClearCell(row, dataCol)
				}
			}
		}
		if a.grid.plan != nil {
			if a.editSet().Deletes[row] {
				if m.Item(locales.RestoreRow()).Chosen() {
					a.clearRowEdit(row)
				}
			} else if m.Item(locales.DeleteRow()).Chosen() {
				a.deleteRow(row)
			}
		}
	}

	handledDouble := false
	tbl := a.withResultFont(ui.Table(c, &a.grid.state, cols, totalRows, func(row, col int) {
		if row < 0 {
			return
		}
		// 新增行渲染在数据行之后。
		if row >= len(displayRows) {
			insIdx := row - len(displayRows)
			if insIdx >= len(editSet.Inserts) {
				return
			}
			if col == 0 {
				ui.Text(c, "+").TextColor(c.Theme().Success)
				return
			}
			if a.edit.InlineOn && a.edit.InlineRow == -1-insIdx && a.edit.InlineCol == col-1 {
				a.inlineCellEditor(c)
				return
			}
			v, ok := editSet.Inserts[insIdx][col-1]
			if !ok {
				txt := ui.Text(c, "").SingleLine()
				if txt.RowDoubleClicked() {
					handledDouble = true
					a.beginEditAt(r, -1-insIdx, col-1)
				}
				return
			}
			txt := ui.Text(c, cellTextDisplay(v)).SingleLine().TextColor(c.Theme().Success)
			if txt.RowDoubleClicked() {
				handledDouble = true
				a.beginEditAt(r, -1-insIdx, col-1)
			}
			return
		}
		ri := displayRows[row]
		if col == 0 {
			label := strconv.Itoa(ri + 1)
			if editSet.Deletes[ri] {
				ui.Text(c, "−"+label).TextColor(c.Theme().Danger)
				return
			}
			ui.Text(c, label).TextColor(c.Theme().TextMuted)
			return
		}
		if col-1 >= len(r.Rows[ri]) {
			return
		}
		if a.edit.InlineOn && a.edit.InlineRow == ri && a.edit.InlineCol == col-1 {
			a.inlineCellEditor(c)
			return
		}
		v := r.Rows[ri][col-1]
		if pending, ok := editSet.Cells[app.CellKey{Row: ri, Col: col - 1}]; ok {
			v = pending
		}
		txt := ui.Text(c, cellTextFor(kind, r, col-1, v)).SingleLine()
		if editSet.Deletes[ri] {
			txt.TextColor(c.Theme().TextMuted)
			return
		}
		if _, dirty := editSet.Cells[app.CellKey{Row: ri, Col: col - 1}]; dirty {
			if row == a.grid.row && col-1 == a.cursorDataCol(r) {
				txt.TextColor(c.Theme().AccentText).Bold()
			} else {
				txt.TextColor(c.Theme().Accent).Bold()
			}
		}
		if txt.RowDoubleClicked() {
			handledDouble = true
			action := a.mouseActionFor(a.resultClickMods)
			a.pointerDouble = false
			switch action {
			case mouseCellEdit:
				a.beginEditAt(r, ri, col-1)
			case mouseCellPreview:
				a.openCellViewer(r, ri, col-1)
			}
		}
	})).Grow(1)
	if tbl.Submitted() && !handledDouble {
		row := a.displayToResult(r, a.grid.row)
		col := a.cursorDataCol(r)
		// 双击落在行号列/空白处时，单元格的 RowDoubleClicked 不会置 handledDouble；
		// 这里必须按同一套鼠标手势处理，否则会回落到旧的硬编码行为。
		if a.pointerDouble {
			action := a.mouseActionFor(a.resultClickMods)
			a.pointerDouble = false
			switch action {
			case mouseCellEdit:
				a.beginEditAt(r, row, col)
			case mouseCellPreview:
				a.openCellViewer(r, row, col)
			}
			return
		}
		// Enter：保持原有键盘习惯（Ctrl+Enter 编辑，Enter 预览）。
		if c.Modifiers()&ui.Ctrl != 0 {
			a.beginEditAt(r, row, col)
		} else {
			a.openCellViewer(r, row, col)
		}
	}
}

// pinButton 显示钉住按钮：当前结果已钉住时图标上强调色，再点一次取消。
func (a *root) pinButton(c *ui.Context) {
	pinned := len(a.tabs.StmtRuns) > 0 && a.tabs.ActiveStmt < len(a.tabs.StmtRuns) && a.tabs.StmtRuns[a.tabs.ActiveStmt].pinned
	label := locales.PinResult()
	col := ui.Transparent
	if pinned {
		label = locales.UnpinResult()
		col = c.Theme().Accent
	}
	if iconButtonPlain(c, iconPin, label, col).Clicked() {
		a.togglePin()
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
			return string(t)
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return t
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
	if a.grid.widthsFor == r && len(a.grid.widths) == len(r.Columns) {
		return a.grid.widths
	}
	a.grid.widthsFor = r
	a.grid.widths = resultWidths(r)
	return a.grid.widths
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
			if u := displayWidth(cutRunes(cellTextDisplay(row[i]), 128)); u > units {
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

// pageToolbar 渲染分页控件：上一页/下一页、每页行数与统计总行数。
func (a *root) pageToolbar(c *ui.Context, r *engine.QueryResult) {
	ps := a.tabs.PageStates[a.currentRuntimeID()]
	if ps == nil || r == nil || !r.IsQuery {
		return
	}
	busy := a.pageBusy(ps)
	dirty := a.editSet().Count() > 0
	disabled := busy || dirty
	if ps.Loading {
		ui.Spinner(c)
		ui.Text(c, locales.PagingLoading()).FontSize(11.5).TextColor(c.Theme().TextMuted)
	}
	if iconButton(c, iconChevronLeft, locales.PagingPrev(), ui.Transparent).
		Disabled(disabled || ps.Page <= 0).Clicked() {
		a.changePage(-1)
	}
	ui.Text(c, locales.PagingPage(ps.Page+1)).FontSize(12).TextColor(c.Theme().TextMuted)
	if iconButton(c, iconChevronRight, locales.PagingNext(), ui.Transparent).
		Disabled(disabled || !a.canNextPage(ps)).Clicked() {
		a.changePage(1)
	}
	pages := int64(1)
	if ps.TotalKnown && ps.Total > 0 {
		pages = (ps.Total + int64(ps.Size) - 1) / int64(ps.Size)
	}
	if ps.TotalKnown {
		ui.Text(c, locales.PagingPages(int(pages))).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	ui.Text(c, locales.PagingPerPage(ps.Size)).FontSize(11.5).TextColor(c.Theme().TextMuted)
	if ps.CountLoading {
		ui.Spinner(c)
		ui.Text(c, locales.PagingCounting()).FontSize(11.5).TextColor(c.Theme().TextMuted)
	} else if !ps.TotalKnown {
		if iconButton(c, iconHash, locales.PagingCountRows(), ui.Transparent).Disabled(busy).Clicked() {
			a.countRows()
		}
	} else {
		ui.Text(c, locales.PagingTotal(int(ps.Total))).FontSize(11.5).TextColor(c.Theme().TextMuted)
	}
}

// pageBusy 报告分页/统计是否正在执行。
func (a *root) pageBusy(ps *pageState) bool {
	if ps == nil {
		return false
	}
	if ps.Loading || ps.CountLoading {
		return true
	}
	if ps.PageRuntime != "" && a.runtimeFor(ps.PageRuntime).Running() {
		return true
	}
	return ps.CountRuntime != "" && a.runtimeFor(ps.CountRuntime).Running()
}

// canNextPage 报告是否还有下一页：未知总数时看驱动是否还有更多行。
func (a *root) canNextPage(ps *pageState) bool {
	if ps == nil || ps.Loading || ps.CountLoading {
		return false
	}
	if ps.TotalKnown {
		return int64(ps.Page+1)*int64(ps.Size) < ps.Total
	}
	return ps.HasMore
}

// changePage 加载上一页/下一页；成功后才替换当前结果行。
func (a *root) changePage(delta int) {
	id := a.currentRuntimeID()
	ps := a.tabs.PageStates[id]
	base := a.currentRuntime()
	if ps == nil || base == nil || a.pageBusy(ps) || a.activeSession() == nil {
		return
	}
	if a.editSet().Count() > 0 {
		a.consoleErr = locales.PageDirty()
		return
	}
	target := ps.Page + delta
	if target < 0 {
		return
	}
	if ps.TotalKnown && int64(target)*int64(ps.Size) >= ps.Total {
		return
	}
	sql, ok := app.PageSQL(ps.BaseSQL, target*ps.Size, ps.Size)
	if !ok {
		a.consoleErr = locales.PageUnsupported()
		return
	}
	expected := base.Result()
	rtID := id + "#page"
	rt := a.runtimeFor(rtID)
	if rt.Running() {
		return
	}
	ps.Loading, ps.PageRuntime = true, rtID
	rt.SetPageSize(ps.Size)
	rt.SetOnDone(func(res *engine.QueryResult, errMsg string, _ time.Duration) {
		apply := func() {
			ps.Loading = false
			if errMsg != "" {
				a.consoleErr = locales.PageFailed(errMsg)
				return
			}
			if a.tabs.PageStates[id] != ps || base.Result() != expected {
				return
			}
			r := base.Result()
			if res == nil || !res.IsQuery || r == nil {
				a.consoleErr = locales.PageNoResult()
				return
			}
			r.Rows = res.Rows
			r.Truncated = res.Truncated
			r.HasMore = res.HasMore
			r.ExecDuration = res.ExecDuration
			r.FetchDuration = res.FetchDuration
			ps.Page = target
			ps.HasMore = res.HasMore && !res.Truncated
			if a.currentRuntimeID() == id {
				a.grid.view.clear()
				a.grid.row = 0
				a.stopInlineEdit()
				a.editSet().Clear()
			}
		}
		if a.hasWindow() {
			win := a.win
			a.safeUpdate(win, apply)
			return
		}
		apply()
	})
	if !rt.RunTimeout(a.activeSession(), sql, a.queryTimeoutFor()) {
		ps.Loading = false
		a.consoleErr = locales.PageStartFailed()
	}
}

// countRows 用 COUNT(*) 包住原始查询统计总行数；显式点击才执行。
func (a *root) countRows() {
	id := a.currentRuntimeID()
	ps := a.tabs.PageStates[id]
	if ps == nil || ps.TotalKnown || a.pageBusy(ps) {
		return
	}
	sql, ok := app.PageCountSQL(a.connKind(), ps.BaseSQL)
	if !ok {
		a.consoleErr = locales.CountUnsupported()
		return
	}
	rtID := id + "#count"
	rt := a.runtimeFor(rtID)
	if rt.Running() {
		return
	}
	ps.CountLoading, ps.CountRuntime = true, rtID
	rt.SetPageSize(1)
	rt.SetOnDone(func(res *engine.QueryResult, errMsg string, _ time.Duration) {
		apply := func() {
			if a.tabs.PageStates[id] != ps {
				return
			}
			ps.CountLoading = false
			if errMsg != "" {
				a.consoleErr = locales.CountFailed(errMsg)
				return
			}
			n, ok := countResult(res)
			if !ok {
				a.consoleErr = locales.CountBadResult()
				return
			}
			ps.Total, ps.TotalKnown = n, true
		}
		if a.hasWindow() {
			win := a.win
			a.safeUpdate(win, apply)
			return
		}
		apply()
	})
	if !rt.RunTimeout(a.activeSession(), sql, a.queryTimeoutFor()) {
		ps.CountLoading = false
		a.consoleErr = locales.CountStartFailed()
	}
}

func countResult(res *engine.QueryResult) (int64, bool) {
	if res == nil || len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return 0, false
	}
	switch v := res.Rows[0][0].(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case uint64:
		if v > 1<<63-1 {
			return 0, false
		}
		return int64(v), true
	case float64:
		return int64(v), v >= 0
	case []byte:
		var n int64
		if _, err := fmt.Sscan(string(v), &n); err == nil && n >= 0 {
			return n, true
		}
	case string:
		var n int64
		if _, err := fmt.Sscan(v, &n); err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}

// filterModal 渲染每列筛选输入。
func (a *root) filterModal(c *ui.Context) {
	if !a.grid.filterOpen {
		return
	}
	ui.Modal(c, &a.grid.filterOpen, func() {
		title := locales.FilterTitle()
		if r := a.currentRuntime().Result(); r != nil && a.grid.filterCol >= 0 && a.grid.filterCol < len(r.Columns) {
			title = locales.FilterTitleColumn(r.Columns[a.grid.filterCol])
		}
		ui.Text(c, title).Bold()
		ui.TextInput(c, &a.grid.filterText).Placeholder(locales.FilterPlaceholder()).AutoFocus()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.grid.filterOpen = false
			}
			if ui.PrimaryButton(c, locales.FilterApply()).Clicked() {
				a.grid.view.setFilter(a.grid.filterCol, a.grid.filterText)
				a.grid.filterOpen = false
			}
		})
	})
}

// recordPageState 在语句执行成功后记录分页状态。
func (a *root) recordPageState(runtimeID, sql string, res *engine.QueryResult) {
	delete(a.tabs.PageStates, runtimeID)
	if res == nil || !res.IsQuery {
		return
	}
	size := a.settings.ResultPrefs.PageSize
	if size <= 0 {
		size = app.DefaultResultPageSize
	}
	if rt := a.runtimeFor(runtimeID); rt != nil {
		if n := rt.PageSize(); n > 0 {
			size = n
		}
	}
	if _, ok := app.PageSQL(sql, 0, size); !ok {
		return
	}
	a.tabs.PageStates[runtimeID] = &pageState{
		Size: size, BaseSQL: sql,
		HasMore: res.HasMore && !res.Truncated,
	}
	a.grid.view.key = ""
}

// canStream 报告能否全量流式导出（驱动实现 RowStreamer 且有可重放的 SQL）。
func (a *root) canStream() bool {
	sess := a.activeSession()
	if sess == nil || strings.TrimSpace(a.tabs.LastExecSQL) == "" {
		return false
	}
	_, ok := sess.(engine.RowStreamer)
	return ok
}

// confirmFullExport 打开「会重新执行查询」的确认弹窗。
func (a *root) confirmFullExport(format string) {
	a.exportState.Format = format
	a.exportState.Confirm = true
}

// exportConfirmModal 渲染全量导出确认。
func (a *root) exportConfirmModal(c *ui.Context) {
	if !a.exportState.Confirm {
		return
	}
	ui.Modal(c, &a.exportState.Confirm, func() {
		ui.Text(c, locales.ExportFullTitle(a.exportState.Format)).Bold()
		ui.Text(c, locales.ExportFullHint()).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.exportState.Confirm = false
			}
			if ui.PrimaryButton(c, locales.ExportChooseFile()).Clicked() {
				format := a.exportState.Format
				a.exportState.Confirm = false
				a.pickExport(format, true)
			}
		})
	})
}

// exportCloseModal 在导出进行中阻止关窗时给用户选择。
func (a *root) exportCloseModal(c *ui.Context) {
	if !a.exportState.CloseConfirm {
		return
	}
	ui.Modal(c, &a.exportState.CloseConfirm, func() {
		ui.Text(c, locales.ExportExitTitle()).Bold()
		ui.Text(c, locales.ExportExitHint()).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.ExportContinue()).Clicked() {
				a.exportState.CloseConfirm = false
			}
			if ui.Button(c, locales.ExportExitAnyway()).TextColor(c.Theme().Danger).Clicked() {
				a.exportState.CloseConfirm = false
				a.exportState.Busy = false
				if a.hasWindow() {
					a.win.Close()
				}
			}
		})
	})
}

// openExportFolder 用系统文件管理器定位最近导出的文件。
func (a *root) openExportFolder() {
	if a.exportState.Path == "" {
		return
	}
	reveal := app.RevealPath
	if a.revealPath != nil {
		reveal = a.revealPath
	}
	if err := reveal(a.exportState.Path); err != nil {
		a.exportState.Status = locales.ExportOpenFolderFailed(err.Error())
	}
}

// exportDefaultName 是导出保存框的默认文件名：YYYYMMDD-HHMM + 扩展名。
func exportDefaultName(now time.Time, ext string) string {
	return now.Format("20060102-1504") + ext
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
			Title:       locales.ExportDialogTitle(),
			DefaultPath: exportDefaultName(time.Now(), ext),
		})
		if err != nil || path == "" {
			return
		}
		a.safeUpdate(win, func() {
			a.exportState.Status = ""
			a.exportState.Path = ""
			a.exportState.Busy = true
			a.exportState.Full = full
			a.exportState.Format = format
			a.exportState.Rows.Store(0)
			a.exportState.CloseConfirm = false
		})
		var progress func(int)
		if full {
			progress = func(n int) {
				a.exportState.Rows.Store(int64(n))
				a.safeUpdate(win, func() {})
			}
		}
		n, err := a.exportToProgress(path, format, full, progress)
		a.safeUpdate(win, func() {
			a.exportState.Busy = false
			a.exportState.CloseConfirm = false
			if err != nil {
				a.exportState.Path = ""
				a.exportState.Status = locales.ExportFailed(err.Error())
				app.LogError("export", "failed", "path", path, "format", format, "err", err.Error())
				return
			}
			a.exportState.Path = path
			a.exportState.Status = locales.ExportDone(n, path)
			app.LogInfo("export", "done", "path", path, "format", format, "rows", n, "full", full)
		})
	}()
}

// pickExportSelection 弹保存对话框导出当前选区（行/列为结果下标）。
func (a *root) pickExportSelection(format string, rows, cols []int) {
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
			Title:       locales.ExportSelectionDialogTitle(),
			DefaultPath: exportDefaultName(time.Now(), ext),
		})
		if err != nil || path == "" {
			return
		}
		a.safeUpdate(win, func() {
			a.exportState.Status = ""
			a.exportState.Path = ""
			a.exportState.Busy = true
			a.exportState.Full = false
			a.exportState.Format = format
			a.exportState.Rows.Store(0)
			a.exportState.CloseConfirm = false
		})
		n, err := a.exportSelectionTo(path, format, rows, cols)
		a.safeUpdate(win, func() {
			a.exportState.Busy = false
			a.exportState.CloseConfirm = false
			if err != nil {
				a.exportState.Path = ""
				a.exportState.Status = locales.ExportFailed(err.Error())
				app.LogError("export", "selection failed", "path", path, "format", format, "err", err.Error())
				return
			}
			a.exportState.Path = path
			a.exportState.Status = locales.ExportSelectionDone(n, path)
			app.LogInfo("export", "selection done", "path", path, "format", format, "rows", n)
		})
	}()
}

// exportSelectionTo 同步导出选区（对话框之外的实现；测试直接调用）。
func (a *root) exportSelectionTo(path, format string, rows, cols []int) (int, error) {
	r := a.currentRuntime().Result()
	if r == nil || !r.IsQuery {
		return 0, errors.New(locales.ExportNoResult())
	}
	table := ""
	if t, ok := a.rowTable(); ok {
		table = t
	}
	if format == "insert" && table == "" {
		return 0, errors.New(locales.ExportNoTable())
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = r.Columns[col]
	}
	data := make([][]any, len(rows))
	for i, row := range rows {
		rec := make([]any, len(cols))
		for j, col := range cols {
			rec[j] = cellAt(r.Rows[row], col)
		}
		data[i] = rec
	}
	return app.ExportRows(path, format, a.connKind(), table, names, data)
}

// exportTo 同步导出（对话框之外的实现；测试直接调用）。
func (a *root) exportTo(path, format string, full bool) (int, error) {
	return a.exportToProgress(path, format, full, nil)
}

// exportToProgress 是 exportTo 的带进度版本；progress 在流式全量导出时按行数回调，
// 用于在界面上显示「正在导出」。非全量导出不回调。
func (a *root) exportToProgress(path, format string, full bool, progress func(int)) (int, error) {
	kind := a.connKind()
	table := ""
	if _, ok := app.PageBody(a.tabs.LastExecSQL); ok {
		if t, ok := a.rowTable(); ok {
			table = t
		}
	}
	if format == "insert" && table == "" {
		return 0, errors.New(locales.ExportNoTable())
	}
	if full {
		streamer, ok := a.activeSession().(engine.RowStreamer)
		if !ok {
			return 0, errors.New(locales.ExportNoStream())
		}
		sql := a.tabs.LastExecSQL
		written := 0
		last := time.Now()
		stream := func(onCols func([]string) error, onRow func([]any) error) error {
			return streamer.Stream(context.Background(), sql, onCols, func(row []any) error {
				if err := onRow(row); err != nil {
					return err
				}
				written++
				if progress != nil && (written%500 == 0 || time.Since(last) >= 200*time.Millisecond) {
					last = time.Now()
					progress(written)
				}
				return nil
			})
		}
		n, err := app.ExportStream(path, format, kind, table, stream)
		if progress != nil {
			progress(n)
		}
		return n, err
	}
	r := a.currentRuntime().Result()
	if r == nil || !r.IsQuery {
		return 0, errors.New(locales.ExportNoResult())
	}
	rows := make([][]any, len(r.Rows))
	copy(rows, r.Rows)
	return app.ExportRows(path, format, kind, table, r.Columns, rows)
}
