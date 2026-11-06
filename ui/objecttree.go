package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
)

var objectKinds = []struct {
	kind  engine.ObjectKind
	label string
}{
	{engine.ObjectTable, "表"},
	{engine.ObjectView, "视图"},
	{engine.ObjectIndex, "索引"},
}

const (
	// objectPageSize 是展开一个对象分组时一次拉取的条数。mygo 的 Tree 不做虚拟化，
	// 页大小压小，保证大库展开时也能流畅渲染；更多内容靠「继续加载」。
	objectPageSize = 1000
	// objectMaxLoaded 是单个分组最多加载的条数（对齐 db-k 的 AUTO_LOAD_CAP），
	// 避免把 6 万张表全塞进内存和非虚拟化的树里。
	objectMaxLoaded = 5000
)

func kindKey(k engine.ObjectKind) string {
	switch k {
	case engine.ObjectTable:
		return "table"
	case engine.ObjectView:
		return "view"
	default:
		return "index"
	}
}

// objectTreeKey 是对象缓存/计数/加载状态的键：连接 + schema（空 = 默认 schema）。
func objectTreeKey(connID, schema string) string {
	if schema == "" {
		return connID
	}
	return connID + "|" + schema
}

func objectGroupKey(base string, kind engine.ObjectKind) string {
	return base + "|" + kindKey(kind)
}

func (a *root) groupOpenPtr(key string, v bool) *bool {
	if a.treeOpen == nil {
		a.treeOpen = map[string]*bool{}
	}
	if p, ok := a.treeOpen[key]; ok {
		return p
	}
	p := new(bool)
	*p = v
	a.treeOpen[key] = p
	return p
}

func (a *root) reloadTree() {
	a.treeGen++
	a.treeData = nil
	a.treeCounts = nil
	a.treeLoading = nil
	a.treePageStarted = nil
	a.treeGroupStale = nil
	a.metaCacheTried = nil
	a.metaCacheLoading = nil
	a.metaCacheStale = nil
	a.treeErr = nil
	if a.metaCache != nil {
		a.metaCache.DeleteAll()
	}
}

func (a *root) setTreeErr(key, msg string) {
	if a.treeErr == nil {
		a.treeErr = map[string]string{}
	}
	a.treeErr[key] = msg
}

// treeCountOf 返回某组的总数；nil 计数视为未知（仍在加载或驱动不支持计数）。
func (a *root) treeCountOf(base string, kind engine.ObjectKind) (int, bool) {
	m, ok := a.treeCounts[base]
	if !ok {
		return 0, false
	}
	return m[kind], true
}

// expandConnAndTree 连上数据源（没连的话）并展开它的树：点数据源名走这里。
func (a *root) expandConnAndTree(p app.ConnProfile) {
	if a.treeExpand == nil {
		a.treeExpand = map[string]bool{}
	}
	a.treeExpand[p.ID] = true
	if a.treeErr != nil {
		delete(a.treeErr, p.ID) // 重连后旧的报错不再作数
	}
	a.loadMetaCache(p)
	a.expandConn(p)
}

// expandConn 保证数据源连着：已经连着只 touch，不重连。
func (a *root) expandConn(p app.ConnProfile) {
	if a.connected(p.ID) {
		a.touchSession(p.ID)
		return
	}
	_ = a.openConn(p)
}

func (a *root) toggleExpandConn(p app.ConnProfile) {
	if a.treeExpand == nil {
		a.treeExpand = map[string]bool{}
	}
	a.treeExpand[p.ID] = !a.treeExpand[p.ID]
	if a.treeExpand[p.ID] {
		if a.treeErr != nil {
			delete(a.treeErr, p.ID)
		}
		a.loadMetaCache(p)
		a.expandConn(p)
	}
}

// loadAllObjectKinds 是驱动不支持计数/分页时的兜底：一次拉全量。
// 只有未知驱动或测试假会话会走这里；内置驱动都实现了 ObjectCounter/PagedObjectLister。
func (a *root) loadAllObjectKinds(sess engine.Session, schema string) (map[engine.ObjectKind][]engine.DBObject, error) {
	if sess == nil {
		return nil, nil
	}
	lister, ok := sess.(engine.ObjectLister)
	if !ok || !sess.Caps().Has(engine.CapObjects) {
		return nil, fmt.Errorf("该数据源不支持对象树")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data := map[engine.ObjectKind][]engine.DBObject{}
	scoped, multi := sess.(engine.SchemaLister)
	for _, g := range objectKinds {
		var objs []engine.DBObject
		var err error
		if schema != "" && multi {
			objs, err = scoped.ObjectsIn(ctx, schema, g.kind)
		} else {
			objs, err = lister.Objects(ctx, g.kind)
		}
		if err != nil {
			return nil, err
		}
		data[g.kind] = objs
	}
	return data, nil
}

// loadAllObjects 异步/同步装载全量对象（兜底路径）并写进缓存。
func (a *root) loadAllObjects(p app.ConnProfile, schema string, base string) {
	if _, ok := a.treeData[base]; ok {
		return
	}
	key := base + "|all"
	if a.treeLoading == nil {
		a.treeLoading = map[string]bool{}
	}
	if a.treeLoading[key] {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return
	}
	a.treeLoading[key] = true
	gen := a.treeGen
	done := func(data map[engine.ObjectKind][]engine.DBObject, err error) {
		delete(a.treeLoading, key)
		if gen != a.treeGen {
			return
		}
		if err != nil {
			a.setTreeErr(base, err.Error())
			return
		}
		if data == nil {
			return
		}
		if a.treeData == nil {
			a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{}
		}
		if a.treeCounts == nil {
			a.treeCounts = map[string]map[engine.ObjectKind]int{}
		}
		a.treeData[base] = data
		counts := map[engine.ObjectKind]int{}
		for kind, objs := range data {
			counts[kind] = len(objs)
		}
		a.treeCounts[base] = counts
		a.saveMetaCache(p)
	}
	if a.win == nil {
		data, err := a.loadAllObjectKinds(sess, schema)
		done(data, err)
		return
	}
	win := a.win
	go func() {
		data, err := a.loadAllObjectKinds(sess, schema)
		win.Update(func() { done(data, err) })
	}()
}

// ensureObjectCounts 只取各类型数量；有 ObjectCounter 用 count 查询，
// 否则退化为全量加载（内置驱动都已实现计数）。
func (a *root) ensureObjectCounts(p app.ConnProfile, schema string) {
	base := objectTreeKey(p.ID, schema)
	if _, ok := a.treeCounts[base]; ok {
		return
	}
	countKey := base + "|counts"
	if a.treeLoading == nil {
		a.treeLoading = map[string]bool{}
	}
	if a.treeLoading[countKey] {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return // 没连上就不画树（侧栏的圆点已经说明状态），不写「未连接」
	}
	if !sess.Caps().Has(engine.CapObjects) {
		a.setTreeErr(base, "该数据源不支持对象树")
		return
	}
	counter, ok := sess.(engine.ObjectCounter)
	if !ok {
		a.loadAllObjects(p, schema, base)
		return
	}
	a.treeLoading[countKey] = true
	gen := a.treeGen
	load := func() (map[engine.ObjectKind]int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return counter.ObjectCounts(ctx, schema)
	}
	done := func(counts map[engine.ObjectKind]int, err error) {
		delete(a.treeLoading, countKey)
		if gen != a.treeGen {
			return
		}
		if err != nil {
			a.setTreeErr(base, err.Error())
			return
		}
		if a.treeCounts == nil {
			a.treeCounts = map[string]map[engine.ObjectKind]int{}
		}
		a.treeCounts[base] = counts
		a.saveMetaCache(p)
	}
	if a.win == nil {
		counts, err := load()
		done(counts, err)
		return
	}
	win := a.win
	go func() {
		counts, err := load()
		win.Update(func() { done(counts, err) })
	}()
}

// ensureObjectPage 只在分组第一次展开时加载第一页，之后由「继续加载」显式追加；
// 避免开着的分组每一帧都自动拉下一页。
func (a *root) ensureObjectPage(p app.ConnProfile, schema string, kind engine.ObjectKind) {
	groupKey := objectGroupKey(objectTreeKey(p.ID, schema), kind)
	if a.treePageStarted == nil {
		a.treePageStarted = map[string]bool{}
	}
	if a.treeGroupStale[groupKey] {
		if a.sessionFor(p.ID) == nil {
			return // 还没连上：保留 stale 标记，连上后再刷新
		}
		delete(a.treeGroupStale, groupKey)
		a.loadObjectPage(p, schema, kind, true)
		return
	}
	if a.treePageStarted[groupKey] {
		return
	}
	if a.sessionFor(p.ID) == nil {
		return // 不留下 started 标记，连上后下一帧再加载
	}
	a.treePageStarted[groupKey] = true
	a.loadObjectPage(p, schema, kind, false)
}

// loadObjectPage 加载某类型分组的下一页；replace=true 时用第一页替换过期缓存。
func (a *root) loadObjectPage(p app.ConnProfile, schema string, kind engine.ObjectKind, replace bool) {
	base := objectTreeKey(p.ID, schema)
	groupKey := objectGroupKey(base, kind)
	if a.treeLoading == nil {
		a.treeLoading = map[string]bool{}
	}
	if a.treeLoading[groupKey] {
		return
	}
	loaded := 0
	if m := a.treeData[base]; m != nil && !replace {
		loaded = len(m[kind])
	}
	total, hasTotal := a.treeCountOf(base, kind)
	if !replace && hasTotal && loaded >= total {
		return
	}
	if loaded >= objectMaxLoaded {
		return
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return
	}
	pager, ok := sess.(engine.PagedObjectLister)
	if !ok {
		a.loadAllObjects(p, schema, base)
		return
	}
	limit := objectPageSize
	if hasTotal && total > 0 && total < limit {
		limit = total
	}
	if !replace && hasTotal && total-loaded < limit {
		limit = total - loaded
	}
	if loaded+limit > objectMaxLoaded {
		limit = objectMaxLoaded - loaded
	}
	if limit <= 0 {
		return
	}
	a.treeLoading[groupKey] = true
	gen := a.treeGen
	load := func() ([]engine.DBObject, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return pager.ObjectsPage(ctx, schema, kind, loaded, limit)
	}
	done := func(objs []engine.DBObject, err error) {
		delete(a.treeLoading, groupKey)
		if gen != a.treeGen {
			return
		}
		if err != nil {
			a.setTreeErr(groupKey, err.Error())
			return
		}
		if a.treeData == nil {
			a.treeData = map[string]map[engine.ObjectKind][]engine.DBObject{}
		}
		if a.treeData[base] == nil {
			a.treeData[base] = map[engine.ObjectKind][]engine.DBObject{}
		}
		if replace {
			a.treeData[base][kind] = objs
		} else {
			a.treeData[base][kind] = append(a.treeData[base][kind], objs...)
		}
		a.saveMetaCache(p)
	}
	if a.win == nil {
		objs, err := load()
		done(objs, err)
		return
	}
	win := a.win
	go func() {
		objs, err := load()
		win.Update(func() { done(objs, err) })
	}()
}

// loadGroupsIn 保留为同步入口（测试/兼容）：取计数并把每个类型的第一页装好。
func (a *root) loadGroupsIn(p app.ConnProfile, schema string) {
	a.ensureObjectCounts(p, schema)
	for _, g := range objectKinds {
		a.ensureObjectPage(p, schema, g.kind)
	}
}

func (a *root) objectGroups(c *ui.Context, p app.ConnProfile) {
	base := objectTreeKey(p.ID, "")
	if err := a.treeErr[base]; err != "" {
		ui.Text(c, err).TextColor(c.Theme().Danger)
		return
	}
	if a.metaCacheLoading[p.ID] {
		ui.Text(c, "加载元数据…").TextColor(c.Theme().TextMuted)
		return
	}
	if a.metaCacheStale[p.ID] && a.connected(p.ID) {
		a.refreshStaleMeta(p)
	}
	// 多 schema 数据源：先列 schema，再在 schema 下列对象分组；
	// 有磁盘缓存时即使还没连上也先把 schema 节点画出来。
	if lister, ok := a.schemaLister(p); ok || len(a.treeSchemas[p.ID]) > 0 {
		a.schemaTree(c, p, lister)
		return
	}
	a.ensureObjectCounts(p, "")
	if err := a.treeErr[base]; err != "" {
		ui.Text(c, err).TextColor(c.Theme().Danger)
		return
	}
	ui.Tree(c, func() {
		a.kindGroups(c, p, "")
	})
}

// schemaLister 返回该连接当前会话的 SchemaLister（未连接/不支持时为 false）。
func (a *root) schemaLister(p app.ConnProfile) (engine.SchemaLister, bool) {
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return nil, false
	}
	lister, ok := sess.(engine.SchemaLister)
	return lister, ok
}

// schemaTree 渲染 schema 列表（懒加载）。
func (a *root) schemaTree(c *ui.Context, p app.ConnProfile, lister engine.SchemaLister) {
	if a.treeSchemas == nil {
		a.treeSchemas = map[string][]string{}
	}
	if _, ok := a.treeSchemas[p.ID]; !ok {
		if lister == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		schemas, err := lister.Schemas(ctx)
		cancel()
		if err != nil {
			ui.Text(c, err.Error()).TextColor(c.Theme().Danger)
			return
		}
		a.treeSchemas[p.ID] = schemas
		a.saveMetaCache(p)
	}
	ui.Tree(c, func() {
		for _, schema := range a.treeSchemas[p.ID] {
			key := p.ID + "|" + schema
			base := objectTreeKey(p.ID, schema)
			open := a.groupOpenPtr(key+"|schema", false)
			ui.TreeItem(c, "▣ "+schema, open, func() {
				a.ensureObjectCounts(p, schema)
				if err := a.treeErr[base]; err != "" {
					ui.TreeItem(c, err, nil, nil)
					return
				}
				a.kindGroups(c, p, schema)
			})
		}
	})
}

// kindGroups 渲染某个 schema（或默认 schema，schema=""）下的对象分组。
// 标题只显示计数，不拉正文；正文在组展开时按页加载。
func (a *root) kindGroups(c *ui.Context, p app.ConnProfile, schema string) {
	base := objectTreeKey(p.ID, schema)
	for _, g := range objectKinds {
		label := g.label
		if total, ok := a.treeCountOf(base, g.kind); ok {
			label = fmt.Sprintf("%s (%d)", g.label, total)
		}
		open := a.groupOpenPtr(base+"|"+kindKey(g.kind), false)
		kind := g.kind
		ui.TreeItem(c, label, open, func() {
			a.ensureObjectPage(p, schema, kind)
			a.objectLeaves(c, p, schema, base, kind)
		})
	}
}

// objectLeaves 渲染某组已加载的对象，以及加载中/失败/继续加载行。
func (a *root) objectLeaves(c *ui.Context, p app.ConnProfile, schema, base string, kind engine.ObjectKind) {
	groupKey := objectGroupKey(base, kind)
	if err := a.treeErr[groupKey]; err != "" {
		ui.TreeItem(c, err, nil, nil)
		return
	}
	var objs []engine.DBObject
	if m := a.treeData[base]; m != nil {
		objs = m[kind]
	}
	if len(objs) == 0 && (a.treeLoading[groupKey] || a.treeLoading[base+"|all"] || a.treeLoading[base+"|counts"]) {
		ui.TreeItem(c, "加载中…", nil, nil)
	}
	for _, o := range objs {
		obj := o
		selKey := base + "|" + obj.Name
		leaf := ui.TreeItem(c, obj.Name, nil, nil).Selected(a.treeSelected == selKey)
		if leaf.Clicked() {
			a.treeSelected = selKey
		}
		if leaf.DoubleClicked() {
			a.previewObject(p, obj)
		}
		leaf.ContextMenu(func(m *ui.Menu) {
			if m.Item("预览数据").Chosen() {
				a.previewObject(p, obj)
			}
			if m.Item("查看 DDL").Chosen() {
				a.showDDL(p, obj)
			}
			if m.Item("插入名称").Chosen() {
				a.code.Insert(obj.Name)
			}
		})
	}
	if total, ok := a.treeCountOf(base, kind); ok && len(objs) < total {
		if len(objs) >= objectMaxLoaded {
			ui.TreeItem(c, fmt.Sprintf("已显示前 %d 个，共 %d 个", len(objs), total), nil, nil)
			return
		}
		label := fmt.Sprintf("继续加载（已 %d / 共 %d）", len(objs), total)
		if ui.TreeItem(c, label, nil, nil).Clicked() {
			a.loadObjectPage(p, schema, kind, false)
		}
	}
}

// previewObject 在新控制台里预览对象数据（SELECT * … LIMIT 200）并立即执行。
func (a *root) previewObject(p app.ConnProfile, o engine.DBObject) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if !a.connected(p.ID) {
		if err := a.openConn(p); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	rec, err := m.Create(p.ID, "预览 "+o.Name)
	if err != nil {
		a.consoleErr = "新建预览控制台失败: " + err.Error()
		return
	}
	a.pendingActivate = rec.ID
	a.pendingText = "SELECT * FROM " + qualifiedName(p.Kind, o) + " LIMIT 200"
	a.pendingExec = true
}

// showDDL 取对象的 DDL 并弹窗（headless 同步，有窗口时后台 + Update）。
func (a *root) showDDL(p app.ConnProfile, o engine.DBObject) {
	if !a.connected(p.ID) {
		if err := a.openConn(p); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	sess := a.sessionFor(p.ID)
	load := func() {
		lister, ok := sess.(engine.ObjectDDLer)
		if !ok || sess == nil || !sess.Caps().Has(engine.CapObjectDDL) {
			a.setDDLNote(o.Name, "该数据源不支持查看 DDL")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ddl, err := lister.ObjectDDL(ctx, o.Kind, o.Name)
		apply := func() {
			if err != nil {
				a.setDDLNote(o.Name, "读取 DDL 失败: "+err.Error())
				app.LogWarn("ddl", "load failed", "object", o.Name, "err", err.Error())
				return
			}
			a.setDDL(o.Name, ddl)
		}
		if a.win != nil {
			win := a.win
			win.Update(apply)
			return
		}
		apply()
	}
	if a.win == nil {
		load()
		return
	}
	go load()
}

// DDL 弹窗的固定尺寸：正文是可滚动编辑器，面板不随 DDL 长短跳动。
const (
	ddlPanelWidth  = 760
	ddlPanelHeight = 520
)

// ddlViewState 返回 DDL 只读编辑器状态（首次用时创建）：SQL 高亮、可选中复制。
func (a *root) ddlViewState() *myeditor.State {
	if a.ddlState == nil {
		buf := myeditor.NewBuffer("")
		buf.SetLanguage(myeditor.LanguageNamed("SQL"))
		a.ddlState = &myeditor.State{
			Buffer:       buf,
			ReadOnly:     true,
			LineNumbers:  myeditor.LineNumbersOff,
			NoActiveLine: true,
			NoFolding:    true,
		}
	}
	return a.ddlState
}

// setDDL 把格式化后的 DDL 装进只读高亮视图并打开弹窗。
func (a *root) setDDL(title, ddl string) {
	a.ddlTitle, a.ddlText, a.ddlOpen = title, ddl, true
	text := ddl
	if out, err := app.FormatSQL(ddl); err == nil {
		text = out
	}
	a.ddlViewState().Buffer.Reset(text)
}

// setDDLNote 在弹窗里显示一行提示（不支持 / 读取失败），不做格式化。
func (a *root) setDDLNote(title, msg string) {
	a.ddlTitle, a.ddlText, a.ddlOpen = title, msg, true
	a.ddlViewState().Buffer.Reset(msg)
}

// ddlPanel 渲染 DDL 弹窗：只读 SQL 编辑器支持高亮与部分选中复制。
func (a *root) ddlPanel(c *ui.Context) {
	if !a.ddlOpen {
		return
	}
	ui.Modal(c, &a.ddlOpen, func() {
		ui.Text(c, "DDL："+a.ddlTitle).Bold().SingleLine()
		myeditor.View(c, a.ddlViewState()).FillWidth().Height(ddlPanelHeight - 110)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "复制全部").Tooltip("复制整段 DDL；先在正文里选中部分再 Ctrl+C 只复制选区").
				Clicked() {
				c.WriteClipboard(a.ddlState.Buffer.String())
			}
			if ui.PrimaryButton(c, "关闭").Clicked() {
				a.ddlOpen = false
			}
		})
	}).Width(ddlPanelWidth).Height(ddlPanelHeight)
}

// qualifiedName 按对象所在的 schema 生成引用名（跨 schema 预览/DDL 用）。
func qualifiedName(kind string, o engine.DBObject) string {
	if o.Schema != "" {
		return app.QuoteIdent(kind, o.Schema) + "." + app.QuoteIdent(kind, o.Name)
	}
	return app.QuoteIdent(kind, o.Name)
}
