package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

// viewerPage 是表视图标签的三个子页。
type viewerPage int

const (
	viewerData viewerPage = iota
	viewerStructure
	viewerDDL
)

// viewerRuntimeID 是表视图网格状态的桶键（与控制台运行态 id 区分开）。
func viewerRuntimeID(id string) string { return "vw:" + id }

// viewerQuery 是数据页的查询状态。
type viewerQuery struct {
	whereIn string         // 输入中的条件
	where   string         // 已应用的条件
	sortBy  []ui.SortOrder // 服务端 ORDER BY（列名）
	offset  int            // 分页偏移
	loading bool
	err     string
	sql     string // 当前结果对应的 SQL（编辑计划用）
}

// viewerTab 是一个临时的表/视图浏览标签：不落盘，应用退出即丢。
// 同一个 (connID, schema, name, kind) 只有一个标签，重复双击只激活已有标签。
type viewerTab struct {
	id     string
	connID string
	schema string
	obj    engine.DBObject

	page viewerPage
	rt   *app.Console // 数据页的独立运行态（进度/停止/结果都复用它）
	grid *gridState

	query viewerQuery
	seq   uint64 // 请求轮次：刷新/翻页后旧结果作废

	cols        []engine.Column
	colsState   ui.ListState
	colsErr     string
	colsLoading bool

	idx        []engine.Index
	idxState   ui.ListState
	idxErr     string
	idxLoading bool

	ddl        string
	ddlErr     string
	ddlLoading bool
	ddlView    *myeditor.State
}

// viewerTabID 是表视图标签的稳定 id。
func viewerTabID(connID, schema string, o engine.DBObject) string {
	return connID + "|" + schema + "|" + o.Name + "|" + kindKey(o.Kind)
}

// openViewerTab 打开（或激活）一个表/视图浏览标签。
func (a *root) openViewerTab(p app.ConnProfile, o engine.DBObject) {
	if o.Kind != engine.ObjectTable && o.Kind != engine.ObjectView {
		return
	}
	id := viewerTabID(p.ID, o.Schema, o)
	if a.tabs.ViewerTabs == nil {
		a.tabs.ViewerTabs = map[string]*viewerTab{}
	}
	t := a.tabs.ViewerTabs[id]
	if t == nil {
		t = &viewerTab{
			id: id, connID: p.ID, schema: o.Schema, obj: o,
			page: viewerData,
			grid: newGridState(),
		}
		a.tabs.ViewerTabs[id] = t
		a.tabs.ViewerOrder = append(a.tabs.ViewerOrder, id)
		app.LogInfo("catalog", "viewer open", "conn", p.Name, "object", o.Name)
	}
	a.activateViewer(id)
}

// activateViewer 切到表视图标签：只把当前网格指针切到该标签自己的状态。
func (a *root) activateViewer(id string) {
	t := a.tabs.ViewerTabs[id]
	if t == nil {
		return
	}
	if a.tabs.ActiveViewer == id {
		a.ensureViewerPage(t)
		return
	}
	if a.activeViewerTab() == nil {
		if err := a.flushActive(); err != nil {
			a.externalFlushFailed(err)
			return
		}
		a.tabs.ConsoleGrid = a.grid
	}
	a.tabs.ActiveViewer = id
	if t.grid == nil {
		t.grid = newGridState()
	}
	a.grid = t.grid
	a.stopInlineEdit()
	a.edit.CellOpen, a.edit.PreviewOpen, a.grid.filterOpen = false, false, false
	a.touchConsoleMRU(viewerRuntimeID(id))
	a.ensureViewerPage(t)
}

// leaveViewer 离开表视图回到控制台（换回控制台自己的网格指针）。
func (a *root) leaveViewer() {
	if a.tabs.ActiveViewer == "" {
		return
	}
	a.tabs.ActiveViewer = ""
	if a.tabs.ConsoleGrid == nil {
		a.tabs.ConsoleGrid = newGridState()
	}
	a.grid = a.tabs.ConsoleGrid
	a.stopInlineEdit()
	a.edit.CellOpen, a.edit.PreviewOpen, a.grid.filterOpen = false, false, false
}

// closeViewerTab 关闭一个表视图标签；关的是当前标签时回落到控制台。
func (a *root) closeViewerTab(id string) {
	t := a.tabs.ViewerTabs[id]
	if t == nil {
		return
	}
	if t.rt != nil {
		t.rt.Cancel()
	}
	delete(a.tabs.ViewerTabs, id)
	delete(a.edit.Sets, viewerRuntimeID(id))
	for i, v := range a.tabs.ViewerOrder {
		if v == id {
			a.tabs.ViewerOrder = append(a.tabs.ViewerOrder[:i], a.tabs.ViewerOrder[i+1:]...)
			break
		}
	}
	if a.tabs.ActiveViewer == id {
		a.tabs.ActiveViewer = ""
		if a.tabs.ConsoleGrid == nil {
			a.tabs.ConsoleGrid = newGridState()
		}
		a.grid = a.tabs.ConsoleGrid
	}
	app.LogInfo("catalog", "viewer close", "object", t.obj.Name)
}

// closeViewersOf 关掉某数据源的全部表视图标签（删连接/同步刷新时用）。
func (a *root) closeViewersOf(connID string) {
	for _, id := range append([]string(nil), a.tabs.ViewerOrder...) {
		if t := a.tabs.ViewerTabs[id]; t != nil && t.connID == connID {
			a.closeViewerTab(id)
		}
	}
}

// viewerKind 返回表视图数据源的种类（引用符/格式化用）。
func (a *root) viewerKind(t *viewerTab) string {
	if k := a.connKindOf(t.connID); k != "" {
		return k
	}
	return "sqlite"
}

// viewerSQL 拼数据页的查询；withLimit=false 时给复制/带走用（不带分页）。
func (a *root) viewerSQL(t *viewerTab, withLimit bool) (string, error) {
	kind := a.viewerKind(t)
	q := "SELECT * FROM " + qualifiedName(kind, t.obj)
	if w := strings.TrimSpace(t.query.where); w != "" {
		q += " WHERE " + w
	}
	if len(t.query.sortBy) > 0 {
		var order []string
		for _, s := range t.query.sortBy {
			o := app.QuoteIdent(kind, s.Column)
			if s.Descending {
				o += " DESC"
			}
			order = append(order, o)
		}
		q += " ORDER BY " + strings.Join(order, ", ")
	}
	if withLimit {
		size := a.settings.ResultPrefs.PageSize
		if size <= 0 {
			size = app.DefaultResultPageSize
		}
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", size, max(t.query.offset, 0))
	}
	// WHERE 是用户输入的片段：拼好后必须仍是一条语句，挡住 `;` 追加语句。
	if n := len(app.SplitStatements(q)); n != 1 {
		return "", errors.New(locales.ViewerWhereInvalid())
	}
	return q, nil
}

// ensureViewerPage 在子页第一次打开时按需加载数据/结构/DDL。
func (a *root) ensureViewerPage(t *viewerTab) {
	switch t.page {
	case viewerData:
		a.ensureViewerData(t)
	case viewerStructure:
		a.viewerLoadColumns(t)
		a.viewerLoadIndexes(t)
	case viewerDDL:
		a.viewerLoadDDL(t)
	}
}

// ensureViewerData 保证连着数据源并读到数据页内容。
func (a *root) ensureViewerData(t *viewerTab) {
	if t.rt != nil && (t.rt.Running() || t.rt.Result() != nil) {
		return
	}
	if t.query.loading {
		return
	}
	a.ensureViewerSession(t, func() { a.viewerReload(t, nil) })
}

// ensureViewerSession 保证该标签的数据源连着；没连就后台连（失败会 toast）。
func (a *root) ensureViewerSession(t *viewerTab, then func()) {
	if a.sessionFor(t.connID) != nil {
		then()
		return
	}
	p, ok := a.store.Get(t.connID)
	if !ok {
		t.query.err = locales.RunConnDeleted()
		return
	}
	t.query.loading = true
	a.connectAsync(p, func(err error) {
		if a.tabs.ViewerTabs[t.id] != t {
			return
		}
		t.query.loading = false
		if err != nil {
			t.query.err = app.ErrorText(err)
			return
		}
		then()
	})
}

// viewerReload 重新执行数据页查询；after 在结果回投后调用（可为 nil）。
func (a *root) viewerReload(t *viewerTab, after func()) {
	if a.tabs.ViewerTabs[t.id] != t {
		return
	}
	if t.rt != nil && t.rt.Running() {
		return // 上一次还没结束，别把它顶掉
	}
	sql, err := a.viewerSQL(t, true)
	if err != nil {
		t.query.loading = false
		t.query.err = err.Error()
		return
	}
	sess := a.sessionFor(t.connID)
	if sess == nil {
		a.ensureViewerSession(t, func() { a.viewerReload(t, after) })
		return
	}
	if t.rt == nil {
		t.rt = a.newConsole()
	}
	t.seq++
	seq := t.seq
	t.query.sql = sql
	t.query.loading, t.query.err = true, ""
	size := a.settings.ResultPrefs.PageSize
	if size <= 0 {
		size = app.DefaultResultPageSize
	}
	t.rt.SetPageSize(size)
	t.rt.SetOnDone(func(_ *engine.QueryResult, errMsg string, _ time.Duration) {
		apply := func() {
			if a.tabs.ViewerTabs[t.id] != t || seq != t.seq {
				return
			}
			t.query.loading = false
			t.query.err = errMsg
			if a.viewerActive() && a.tabs.ActiveViewer == t.id {
				a.grid.editKey = "" // 结果变了，可编辑计划重算
			}
			if after != nil {
				after()
			}
		}
		if a.hasWindow() {
			win := a.win
			a.safeUpdate(win, apply)
			return
		}
		apply()
	})
	timeout := a.viewerQueryTimeout(t)
	if !t.rt.RunTimeout(sess, sql, timeout) {
		t.query.loading = false
		t.query.err = locales.ViewerStartFailed()
	}
}

// viewerQueryTimeout 返回该数据源配置的查询超时（0 = 运行态默认 30s）。
func (a *root) viewerQueryTimeout(t *viewerTab) time.Duration {
	if a.store == nil {
		return 0
	}
	if p, ok := a.store.Get(t.connID); ok && p.QueryTimeout > 0 {
		return time.Duration(p.QueryTimeout) * time.Second
	}
	return 0
}

// refreshGrid 在提交改动/刷新后重跑当前网格（控制台或表视图）。
func (a *root) refreshGrid(after func()) {
	if t := a.activeViewerTab(); t != nil {
		a.viewerReload(t, after)
		return
	}
	a.refreshResult(after)
}

// viewerLoadColumns 读字段清单（结构页）。
func (a *root) viewerLoadColumns(t *viewerTab) {
	if t.cols != nil || t.colsLoading {
		return
	}
	sess := a.sessionFor(t.connID)
	if sess == nil {
		a.ensureViewerSession(t, func() { a.viewerLoadColumns(t) })
		return
	}
	if !sess.Caps().Has(engine.CapColumns) {
		t.colsErr = locales.ViewerColumnsUnsupported()
		return
	}
	t.colsLoading = true
	load := func() ([]engine.Column, error) {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		cols, err := engine.ColumnsOf(ctx, sess, t.schema, t.obj.Name)
		if errors.Is(err, engine.ErrUnsupported) {
			return nil, nil
		}
		return cols, err
	}
	done := func(cols []engine.Column, err error) {
		t.colsLoading = false
		if a.tabs.ViewerTabs[t.id] != t {
			return
		}
		if err != nil {
			t.colsErr = app.ErrorText(err)
			return
		}
		t.cols = cols
	}
	var (
		cols []engine.Column
		err  error
	)
	a.runBackground(func() {
		cols, err = load()
	}, func() {
		done(cols, err)
	})
}

// viewerLoadIndexes 读索引清单（结构页）。
func (a *root) viewerLoadIndexes(t *viewerTab) {
	if t.idx != nil || t.idxLoading {
		return
	}
	sess := a.sessionFor(t.connID)
	if sess == nil {
		a.ensureViewerSession(t, func() { a.viewerLoadIndexes(t) })
		return
	}
	if !sess.Caps().Has(engine.CapIndexes) {
		t.idxErr = locales.ViewerIndexesUnsupported()
		return
	}
	t.idxLoading = true
	load := func() ([]engine.Index, error) {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		idx, err := engine.IndexesOf(ctx, sess, t.schema, t.obj.Name)
		if errors.Is(err, engine.ErrUnsupported) {
			return nil, nil
		}
		return idx, err
	}
	done := func(idx []engine.Index, err error) {
		t.idxLoading = false
		if a.tabs.ViewerTabs[t.id] != t {
			return
		}
		if err != nil {
			t.idxErr = app.ErrorText(err)
			return
		}
		if idx == nil {
			idx = []engine.Index{}
		}
		t.idx = idx
	}
	var (
		idx []engine.Index
		err error
	)
	a.runBackground(func() {
		idx, err = load()
	}, func() {
		done(idx, err)
	})
}

// viewerLoadDDL 读对象 DDL（与 Ctrl+Q 弹窗同源）。
func (a *root) viewerLoadDDL(t *viewerTab) {
	if t.ddl != "" || t.ddlErr != "" || t.ddlLoading {
		return
	}
	sess := a.sessionFor(t.connID)
	if sess == nil {
		a.ensureViewerSession(t, func() { a.viewerLoadDDL(t) })
		return
	}
	t.ddlLoading = true
	load := func() (string, bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		return fetchObjectDDL(ctx, sess, t.obj)
	}
	done := func(ddl string, supported bool, err error) {
		t.ddlLoading = false
		if a.tabs.ViewerTabs[t.id] != t {
			return
		}
		switch {
		case !supported:
			t.ddlErr = locales.ViewerDDLUnsupported()
		case err != nil:
			t.ddlErr = locales.TreeDDLFailed(app.ErrorText(err))
		default:
			t.ddl = ddl
		}
	}
	var (
		ddl string
		ok  bool
		err error
	)
	a.runBackground(func() {
		ddl, ok, err = load()
	}, func() {
		done(ddl, ok, err)
	})
}

// viewerPane 渲染表视图标签的内容区。
func (a *root) viewerPane(c *ui.Context) {
	t := a.activeViewerTab()
	if t == nil {
		return
	}
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Segmented(c, (*int)(&t.page), locales.ViewerData(), locales.ViewerStructure(), locales.ViewerDDL()).
				Changed() {
				a.ensureViewerPage(t)
			}
			ui.Spacer(c)
			if icon := objectKindIcon(t.obj.Kind); icon != nil {
				ui.Icon(c, icon).FontSize(14).TextColor(c.Theme().TextMuted)
			}
			label := t.obj.Name
			if t.schema != "" {
				label = t.schema + "." + t.obj.Name
			}
			ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().
				Tooltip(locales.ViewerTabTooltip(a.connName(t.connID), label))
		})
		switch t.page {
		case viewerData:
			a.viewerDataPage(c, t)
		case viewerStructure:
			a.viewerStructurePage(c, t)
		case viewerDDL:
			a.viewerDDLPage(c, t)
		}
	})
}

// connName 返回数据源展示名（连接已删除时为空）。
func (a *root) connName(connID string) string {
	if a.store == nil {
		return ""
	}
	if p, ok := a.store.Get(connID); ok {
		return p.Name
	}
	return ""
}

// viewerDataPage 渲染数据页。
func (a *root) viewerDataPage(c *ui.Context, t *viewerTab) {
	if t.query.loading && (t.rt == nil || !t.rt.Running()) {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.ViewerLoading()).FontSize(12).TextColor(c.Theme().TextMuted)
		})
	}
	if t.rt != nil && t.rt.Running() {
		rows, bytes := t.rt.Progress()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.ViewerLoading())
			if rows > 0 {
				detail := locales.ResultReceivedRows(rows)
				if bytes > 0 {
					detail += " · " + humanBytes(bytes)
				}
				ui.Text(c, detail).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if ui.Button(c, locales.ResultStop()).Clicked() {
				t.rt.Cancel()
			}
		})
	}
	if t.query.err != "" {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, t.query.err).TextColor(c.Theme().Danger).SingleLine().Tooltip(t.query.err)
			if ui.Button(c, locales.ViewerRetry()).Clicked() {
				a.viewerReload(t, nil)
			}
		})
	}
	r := a.gridRuntimeResult(t)
	if r == nil {
		if !t.query.loading && t.query.err == "" {
			ui.Text(c, locales.ResultRunHint()).TextColor(c.Theme().TextMuted)
		}
		return
	}
	a.ensureEditState(r)
	a.viewerDataToolbar(c, t, r)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Toolbar(c, func() { a.editToolbar(c, r) }).Padding(0).Shrink(0).Label(locales.ResultActions())
		if a.edit.Status != "" {
			ui.Text(c, a.edit.Status).FontSize(12).TextColor(c.Theme().Success).SingleLine()
		}
		if r.Truncated {
			ui.Badge(c, locales.ResultTruncated(a.settings.ResultPrefs.BudgetMB))
		}
	})
	a.gridTable(c, r, gridViewer)
	a.viewerDataFooter(c, t, r)
}

// gridRuntimeResult 返回表视图运行态的结果（无结果时 nil）。
func (a *root) gridRuntimeResult(t *viewerTab) *engine.QueryResult {
	if t.rt == nil {
		return nil
	}
	return t.rt.Result()
}

// viewerDataToolbar 是数据页的第一行：WHERE、排序、刷新、复制 SQL、带去控制台、翻页。
func (a *root) viewerDataToolbar(c *ui.Context, t *viewerTab, r *engine.QueryResult) {
	th := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		box := ui.Row(c).Gap(6).Grow(1).Padding(3, 6).Radius(th.Radius).Background(th.Surface).
			Border(1, th.Border)
		box.Children(func() {
			ui.Icon(c, iconSearch).FontSize(13).TextColor(th.TextMuted)
			ui.Text(c, "WHERE").Font("monospace").FontSize(11.5).TextColor(th.TextMuted)
			a.viewerWhereField(c, t, r)
			if t.query.where != "" {
				if iconButtonPlain(c, iconClose, locales.ViewerClearWhere(), ui.Transparent).Clicked() {
					t.query.where, t.query.whereIn = "", ""
					t.query.offset = 0
					a.viewerReload(t, nil)
				}
			}
		})
		// 排序：单列服务端 ORDER BY；再点同一列切降序/取消。
		sortLabel := locales.ViewerSort()
		if len(t.query.sortBy) > 0 {
			s := t.query.sortBy[0]
			sortLabel += " · " + s.Column
			if s.Descending {
				sortLabel += " ↓"
			} else {
				sortLabel += " ↑"
			}
		}
		ui.Button(c, sortLabel).FontSize(12).Menu(func(m *ui.Menu) {
			for _, name := range r.Columns {
				label := name
				desc := false
				if len(t.query.sortBy) > 0 && t.query.sortBy[0].Column == name {
					desc = t.query.sortBy[0].Descending
					if desc {
						label += " ↓"
					} else {
						label += " ↑"
					}
				}
				if !m.Item(label).Chosen() {
					continue
				}
				switch {
				case len(t.query.sortBy) > 0 && t.query.sortBy[0].Column == name && !desc:
					t.query.sortBy = []ui.SortOrder{{Column: name, Descending: true}}
				case len(t.query.sortBy) > 0 && t.query.sortBy[0].Column == name && desc:
					t.query.sortBy = nil
				default:
					t.query.sortBy = []ui.SortOrder{{Column: name}}
				}
				t.query.offset = 0
				a.viewerReload(t, nil)
				return
			}
			if len(t.query.sortBy) > 0 {
				m.Separator()
				if m.Item(locales.ViewerSortClear()).Chosen() {
					t.query.sortBy = nil
					t.query.offset = 0
					a.viewerReload(t, nil)
				}
			}
		})
		if iconButtonPlain(c, iconRefresh, locales.ViewerRefresh(), ui.Transparent).Clicked() {
			if a.editSet().Count() > 0 {
				a.toastError(locales.PageDirty())
			} else {
				a.viewerReload(t, nil)
			}
		}
		if iconButtonPlain(c, iconCopy, locales.ViewerCopySQL(), ui.Transparent).Clicked() {
			if sql, err := a.viewerSQL(t, false); err == nil {
				c.WriteClipboard(sql)
				c.Toast(locales.ViewerCopied())
			}
		}
		if iconButtonPlain(c, iconTerminal, locales.ViewerOpenInConsole(), ui.Transparent).Clicked() {
			if sql, err := a.viewerSQL(t, false); err == nil {
				a.openSQLInConsole(t.connID, sql+"\n")
			}
		}
		ui.Divider(c)
		if iconButton(c, iconChevronLeft, locales.PagingPrev(), ui.Transparent).
			Disabled(t.query.loading || t.query.offset <= 0).Clicked() {
			t.query.offset = max(t.query.offset-a.settings.ResultPrefs.PageSize, 0)
			a.viewerReload(t, nil)
		}
		pageNo := t.query.offset/a.pageSize() + 1
		ui.Text(c, locales.PagingPage(pageNo)).FontSize(12).TextColor(th.TextMuted)
		if iconButton(c, iconChevronRight, locales.PagingNext(), ui.Transparent).
			Disabled(t.query.loading || !a.viewerHasMore(t)).Clicked() {
			t.query.offset += a.pageSize()
			a.viewerReload(t, nil)
		}
	})
}

// viewerWhereField 是 WHERE 条件输入框：输入当前词时补全结果集的列名。
// 选中候选后只替换光标所在的词，条件其余部分保持不变。
func (a *root) viewerWhereField(c *ui.Context, t *viewerTab, r *engine.QueryResult) {
	kind := a.viewerKind(t)
	cb := ui.ComboboxBase(c, &t.query.whereIn)
	in := cb.Input.FontSize(12).Fill().Padding(2, 0).
		Placeholder(locales.ViewerWherePlaceholder()).Label(locales.ViewerWhereLabel()).
		AutoComplete(ui.AutoCompleteList)

	_, caret := in.TextAnchorCaret()
	start, end, prefix := viewerFilterWordAt(t.query.whereIn, caret)
	columns := viewerFilterColumns(r.Columns, prefix, cb.Filtering())
	if sqlInComment(t.query.whereIn, caret, kind) || sqlInString(t.query.whereIn, caret, kind) {
		columns = nil
	}
	cb.Popup(func(panel ui.Element) {
		th := c.Theme()
		panel.Padding(4).Radius(th.Radius).Background(th.Background).Border(1, th.Border)
		for _, name := range columns {
			item := cb.Item(name).Padding(3, 8).Radius(th.Radius)
			if item.Highlighted() {
				item.Background(th.Accent.Alpha(0.12))
			}
			item.Children(func() {
				ui.Text(c, name).FontSize(12).SingleLine()
			})
		}
	})
	if chosen, ok := cb.Chosen(); ok {
		in.ReplaceText(start, end, viewerFilterInsert(kind, chosen))
	}
	if in.Submitted() {
		t.query.where = strings.TrimSpace(t.query.whereIn)
		t.query.offset = 0
		a.viewerReload(t, nil)
	}
}

// viewerFilterWordAt 返回光标所在标识符的起止（rune 偏移）与光标前的词前缀。
func viewerFilterWordAt(text string, caret int) (start, end int, prefix string) {
	rs := []rune(text)
	caret = min(max(caret, 0), len(rs))
	start, end = caret, caret
	for start > 0 && completionWordRune(rs[start-1]) {
		start--
	}
	for end < len(rs) && completionWordRune(rs[end]) {
		end++
	}
	return start, end, string(rs[start:caret])
}

// viewerFilterColumns 按前缀过滤列名；刚点开（没有键入）时给全部列。
// 已经完整输入列名时不再提示它自己，让下一次 Enter 直接应用条件。
func viewerFilterColumns(columns []string, prefix string, filtering bool) []string {
	if filtering && prefix == "" {
		return nil
	}
	out := make([]string, 0, len(columns))
	lower := strings.ToLower(prefix)
	for _, name := range columns {
		if name == "" {
			continue
		}
		if filtering {
			if !strings.HasPrefix(strings.ToLower(name), lower) || strings.EqualFold(name, prefix) {
				continue
			}
		}
		out = append(out, name)
	}
	return out
}

// viewerFilterInsert 返回插入条件的列名：普通标识符原样，其余按方言加引号。
func viewerFilterInsert(kind, name string) string {
	simple := name != ""
	for i, r := range name {
		if r == '_' || r == '$' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		simple = false
		break
	}
	if simple {
		return name
	}
	return app.QuoteIdent(kind, name)
}

// pageSize 返回结果页大小（设置里的每页行数）。
func (a *root) pageSize() int {
	if a.settings.ResultPrefs.PageSize > 0 {
		return a.settings.ResultPrefs.PageSize
	}
	return app.DefaultResultPageSize
}

// viewerHasMore 报告当前页之后是否还有行。
func (a *root) viewerHasMore(t *viewerTab) bool {
	if t.query.loading {
		return false
	}
	r := a.gridRuntimeResult(t)
	if r == nil || r.Truncated {
		return false
	}
	return len(r.Rows) >= a.pageSize()
}

// viewerDataFooter 显示已读行数与页大小。
func (a *root) viewerDataFooter(c *ui.Context, t *viewerTab, r *engine.QueryResult) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.ViewerRowsRead(len(r.Rows))).FontSize(11.5).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		ui.Text(c, locales.PagingPerPage(a.pageSize())).FontSize(11.5).TextColor(c.Theme().TextMuted)
	})
}

// viewerStructurePage 渲染结构页：字段与索引两张表格。
func (a *root) viewerStructurePage(c *ui.Context, t *viewerTab) {
	a.viewerLoadColumns(t)
	a.viewerLoadIndexes(t)
	th := c.Theme()
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Text(c, locales.ViewerFieldsTitle()).FontSize(12).Bold()
		switch {
		case t.colsErr != "":
			ui.Text(c, t.colsErr).TextColor(th.TextMuted).Padding(4)
		case t.colsLoading || t.cols == nil:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.ViewerLoading()).FontSize(12).TextColor(th.TextMuted)
			})
		default:
			cols := []ui.TableColumn{
				{Title: locales.ViewerColName(), ID: "name", Width: 220},
				{Title: locales.ViewerColType(), ID: "type", Width: 220},
				{Title: locales.ViewerColNullable(), ID: "null", Width: 90, Fixed: true},
				{Title: locales.ViewerColPrimaryKey(), ID: "pk", Width: 90, Fixed: true},
				{Title: locales.ViewerColComment(), ID: "comment", MinWidth: 160},
			}
			ui.Table(c, &t.colsState, cols, len(t.cols), func(row, col int) {
				if row < 0 || row >= len(t.cols) {
					return
				}
				cl := t.cols[row]
				switch col {
				case 0:
					ui.Text(c, cl.Name).SingleLine()
				case 1:
					ui.Text(c, cl.Type).Font("monospace").FontSize(12).SingleLine()
				case 2:
					if cl.Nullable {
						ui.Text(c, "NULL").FontSize(12).TextColor(th.TextMuted)
					} else {
						ui.Text(c, "NOT NULL").FontSize(12)
					}
				case 3:
					if cl.PrimaryKey {
						ui.Text(c, "PK").FontSize(12).TextColor(th.Warning)
					}
				case 4:
					ui.Text(c, cl.Comment).FontSize(12).TextColor(th.TextMuted).SingleLine().
						Tooltip(cl.Comment)
				}
			}).Grow(1)
		}

		ui.Text(c, locales.ViewerIndexesTitle()).FontSize(12).Bold()
		switch {
		case t.idxErr != "":
			ui.Text(c, t.idxErr).TextColor(th.TextMuted).Padding(4)
		case t.idxLoading || t.idx == nil:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.ViewerLoading()).FontSize(12).TextColor(th.TextMuted)
			})
		case len(t.idx) == 0:
			ui.Text(c, locales.ViewerNoIndexes()).FontSize(12).TextColor(th.TextMuted).Padding(4)
		default:
			idxCols := []ui.TableColumn{
				{Title: locales.ViewerIndexName(), ID: "name", Width: 200},
				{Title: locales.ViewerIndexColumns(), ID: "columns", MinWidth: 200},
				{Title: locales.ViewerIndexUnique(), ID: "unique", Width: 70, Fixed: true},
				{Title: locales.ViewerIndexPrimary(), ID: "primary", Width: 70, Fixed: true},
				{Title: locales.ViewerIndexType(), ID: "type", Width: 110},
				{Title: locales.ViewerIndexPredicate(), ID: "predicate", MinWidth: 160},
			}
			ui.Table(c, &t.idxState, idxCols, len(t.idx), func(row, col int) {
				if row < 0 || row >= len(t.idx) {
					return
				}
				ix := t.idx[row]
				switch col {
				case 0:
					ui.Text(c, ix.Name).SingleLine()
				case 1:
					ui.Text(c, strings.Join(ix.Columns, ", ")).Font("monospace").FontSize(12).
						SingleLine().Tooltip(strings.Join(ix.Columns, ", "))
				case 2:
					if ix.Unique {
						ui.Icon(c, iconCheck).FontSize(13).TextColor(th.Success)
					}
				case 3:
					if ix.Primary {
						ui.Icon(c, iconCheck).FontSize(13).TextColor(th.Warning)
					}
				case 4:
					ui.Text(c, ix.Type).FontSize(12).TextColor(th.TextMuted).SingleLine()
				case 5:
					ui.Text(c, ix.Predicate).FontSize(12).TextColor(th.TextMuted).SingleLine().
						Tooltip(ix.Predicate)
				}
			}).Grow(1)
		}
	})
}

// viewerDDLPage 渲染 DDL 页：只读高亮编辑器（与 Ctrl+Q 弹窗同源）。
func (a *root) viewerDDLPage(c *ui.Context, t *viewerTab) {
	a.viewerLoadDDL(t)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		if iconButtonPlain(c, iconCopy, locales.TreeCopyAll(), ui.Transparent).Clicked() && t.ddl != "" {
			c.WriteClipboard(t.ddl)
			c.Toast(locales.ViewerCopied())
		}
		if iconButtonPlain(c, iconTerminal, locales.ViewerOpenInConsole(), ui.Transparent).Clicked() && t.ddl != "" {
			a.openSQLInConsole(t.connID, t.ddl+"\n")
		}
		ui.Spacer(c)
	})
	switch {
	case t.ddlErr != "":
		ui.Text(c, t.ddlErr).TextColor(c.Theme().TextMuted).Padding(4)
	case t.ddlLoading || t.ddl == "":
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.ViewerLoading()).FontSize(12).TextColor(c.Theme().TextMuted)
		})
	default:
		if t.ddlView == nil {
			buf := myeditor.NewBuffer(t.ddl)
			buf.SetLanguage(sqlLanguageForKind(a.viewerKind(t)))
			t.ddlView = &myeditor.State{
				Buffer:       buf,
				ReadOnly:     true,
				LineNumbers:  myeditor.LineNumbersOff,
				NoActiveLine: true,
				NoFolding:    true,
			}
		} else if t.ddlView.Buffer.String() != t.ddl {
			t.ddlView.Buffer.Reset(t.ddl)
			t.ddlView.Buffer.SetLanguage(sqlLanguageForKind(a.viewerKind(t)))
		}
		myeditor.View(c, t.ddlView).Grow(1).FillWidth()
	}
}

// openSQLInConsole 把 SQL 带到该数据源的控制台：有控制台就追加并激活，没有就新建。
func (a *root) openSQLInConsole(connID, sql string) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if recs := m.ConsolesOf(connID); len(recs) > 0 {
		target := recs[0]
		if cur, ok := a.store.GetConsole(a.tabs.ActiveConsole); ok && cur.ConnectionID == connID {
			target = cur
		}
		a.deferActivateConsole(target.ID, sql)
		return
	}
	rec, err := m.Create(connID, "")
	if err != nil {
		a.toastError(locales.ConsoleCreateFailed(err.Error()))
		return
	}
	a.deferActivateConsole(rec.ID, sql)
}
