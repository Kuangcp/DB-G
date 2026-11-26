package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

var objectKinds = []struct {
	kind  engine.ObjectKind
	label func() string
}{
	{engine.ObjectTable, locales.ObjectKindTable},
	{engine.ObjectView, locales.ObjectKindView},
	{engine.ObjectIndex, locales.ObjectKindIndex},
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

// groupOpen 是搜索期间强制展开的分组开关：搜索时返回新的 true 指针，
// 不污染用户平时折叠/展开的状态；搜索清空后恢复原来的 treeOpen。
func (a *root) groupOpen(key string, v bool) *bool {
	if a.objSearchActive() {
		open := true
		return &open
	}
	return a.groupOpenPtr(key, v)
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
	a.aiDDL = nil
	a.aiRelations = nil
	a.treeData = nil
	a.treeCounts = nil
	a.treeLoading = nil
	a.treePageStarted = nil
	a.treeGroupStale = nil
	a.treeSchemas = map[string][]string{}
	a.schemaLoading = map[string]bool{}
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

// expandConn 保证数据源连着：已经连着只 touch，不重连；没连就后台连，不阻塞这一帧。
func (a *root) expandConn(p app.ConnProfile) {
	if a.connected(p.ID) {
		a.touchSession(p.ID)
		return
	}
	a.connectAsync(p, nil)
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
		return nil, errors.New(locales.TreeUnsupported())
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
		a.safeUpdate(win, func() { done(data, err) })
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
		a.setTreeErr(base, locales.TreeUnsupported())
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
		a.safeUpdate(win, func() { done(counts, err) })
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
		a.safeUpdate(win, func() { done(objs, err) })
	}()
}

// loadGroupsIn 保留为同步入口（测试/兼容）：取计数并把每个类型的第一页装好。
func (a *root) loadGroupsIn(p app.ConnProfile, schema string) {
	a.ensureObjectCounts(p, schema)
	for _, g := range objectKinds {
		a.ensureObjectPage(p, schema, g.kind)
	}
}

func (a *root) objectGroups(c *ui.Context, p app.ConnProfile, depth int) {
	base := objectTreeKey(p.ID, "")
	if err := a.treeErr[base]; err != "" {
		treeInfoRow(c, err, depth, c.Theme().Danger)
		return
	}
	if a.metaCacheLoading[p.ID] {
		treeInfoRow(c, locales.TreeLoadingMeta(), depth, c.Theme().TextMuted)
		return
	}
	if a.connOpeningNow(p.ID) && !a.connected(p.ID) {
		treeInfoRow(c, locales.SessionConnecting(), depth, c.Theme().TextMuted)
	}
	if a.metaCacheStale[p.ID] && a.connected(p.ID) {
		a.refreshStaleMeta(p)
	}
	// 多 schema 数据源：先列 schema，再在 schema 下列对象分组；
	// 有磁盘缓存时即使还没连上也先把 schema 节点画出来。
	if lister, ok := a.schemaLister(p); ok || len(a.treeSchemas[p.ID]) > 0 {
		a.schemaTree(c, p, lister, depth)
		return
	}
	a.ensureObjectCounts(p, "")
	if err := a.treeErr[base]; err != "" {
		treeInfoRow(c, err, depth, c.Theme().Danger)
		return
	}
	a.kindGroups(c, p, "", depth)
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
func (a *root) schemaTree(c *ui.Context, p app.ConnProfile, lister engine.SchemaLister, depth int) {
	if a.treeSchemas == nil {
		a.treeSchemas = map[string][]string{}
	}
	if _, ok := a.treeSchemas[p.ID]; !ok {
		a.ensureSchemas(p)
		if a.schemaLoading[p.ID] {
			treeInfoRow(c, locales.TreeLoadingSchemas(), depth, c.Theme().TextMuted)
			return
		}
		if lister == nil {
			return
		}
	}
	for _, schema := range a.treeSchemas[p.ID] {
		if a.searchFiltersObjects(p) && !a.schemaHasMatch(p.ID, schema) {
			continue
		}
		key := p.ID + "|" + schema
		base := objectTreeKey(p.ID, schema)
		open := a.groupOpen(key+"|schema", false)
		row := treeGroupRow(c, "▣ "+schema, open, depth, nil, c.Theme().TextMuted, nil)
		if row.Clicked() {
			a.setActiveSchema(p, schema)
		}
		if !*open {
			continue
		}
		a.ensureObjectCounts(p, schema)
		if err := a.treeErr[base]; err != "" {
			treeInfoRow(c, err, depth+1, c.Theme().Danger)
			continue
		}
		a.kindGroups(c, p, schema, depth+1)
	}
}

// objectKindIcon 返回表/视图/索引分组的图标。
func objectKindIcon(k engine.ObjectKind) *ui.SVG {
	switch k {
	case engine.ObjectTable:
		return iconTable
	case engine.ObjectView:
		return iconView
	default:
		return iconIndex
	}
}

// kindGroups 渲染某个 schema（或默认 schema，schema=""）下的对象分组。
// 标题只显示计数，不拉正文；正文在组展开时按页加载。
func (a *root) kindGroups(c *ui.Context, p app.ConnProfile, schema string, depth int) {
	base := objectTreeKey(p.ID, schema)
	searching := a.searchFiltersObjects(p)
	for _, g := range objectKinds {
		label := g.label()
		if searching {
			n := 0
			for _, o := range a.treeData[base][g.kind] {
				if a.objMatch(o.Name) {
					n++
				}
			}
			if n == 0 {
				continue
			}
			label = fmt.Sprintf("%s (%d)", g.label(), n)
		} else if total, ok := a.treeCountOf(base, g.kind); ok {
			label = fmt.Sprintf("%s (%d)", g.label(), total)
		}
		open := a.groupOpen(base+"|"+kindKey(g.kind), false)
		kind := g.kind
		treeGroupRow(c, label, open, depth, objectKindIcon(kind), c.Theme().TextMuted, nil)
		if !*open {
			continue
		}
		a.ensureObjectPage(p, schema, kind)
		a.objectLeaves(c, p, schema, base, kind, depth+1)
	}
}

// objectLeaves 渲染某组已加载的对象，以及加载中/失败/继续加载行。
func (a *root) objectLeaves(c *ui.Context, p app.ConnProfile, schema, base string, kind engine.ObjectKind, depth int) {
	t := c.Theme()
	groupKey := objectGroupKey(base, kind)
	if err := a.treeErr[groupKey]; err != "" {
		treeInfoRow(c, err, depth, t.Danger)
		return
	}
	var objs []engine.DBObject
	if m := a.treeData[base]; m != nil {
		objs = m[kind]
	}
	searching := a.searchFiltersObjects(p)
	if searching {
		kept := objs[:0:0]
		for _, o := range objs {
			if a.objMatch(o.Name) {
				kept = append(kept, o)
			}
		}
		objs = kept
	}
	if !searching && !a.objSearchActive() && len(objs) == 0 && (a.treeLoading[groupKey] || a.treeLoading[base+"|all"] || a.treeLoading[base+"|counts"]) {
		treeInfoRow(c, locales.TreeLoading(), depth, t.TextMuted)
	}
	for _, o := range objs {
		obj := o
		selKey := base + "|" + obj.Name
		leaf := treeRowBase(c, obj.Name).PaddingStart(float32(4 + depth*folderIndent))
		if a.treeSelected == selKey {
			leaf.Background(t.Accent.Alpha(0.12))
		}
		leaf.Children(func() {
			ui.Text(c, obj.Name).SingleLine()
			ui.Spacer(c)
		})
		if leaf.Clicked() {
			a.treeSelected = selKey
			a.selConn, a.selObj, a.hasSelObj = p, obj, true
			// Ctrl+点击表/视图直接打开现有的 DDL 弹窗；双击的第二下跳过，
			// 避免一次双击发出两次 DDL 请求。
			if leaf.ClickModifiers()&ui.Ctrl != 0 && !leaf.DoubleClicked() &&
				(obj.Kind == engine.ObjectTable || obj.Kind == engine.ObjectView) {
				a.showDDL(p, obj)
			}
		}
		if leaf.DoubleClicked() {
			a.openViewerTab(p, obj)
		}
		leaf.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.TreePreviewData()).Chosen() {
				a.openViewerTab(p, obj)
			}
			if m.Item(locales.TreeInsertPreviewSQL()).Chosen() {
				a.previewObject(p, obj)
			}
			if m.Item(locales.TreeViewDDL()).Chosen() {
				a.showDDL(p, obj)
			}
			if obj.Kind == engine.ObjectTable && m.Item(locales.TreeViewStats()).Chosen() {
				a.showStats(p, obj)
			}
			if m.Item(locales.TreeInsertName()).Chosen() {
				a.code.Insert(obj.Name)
			}
		})
	}
	if searching {
		return
	}
	if total, ok := a.treeCountOf(base, kind); ok && len(objs) < total {
		if len(objs) >= objectMaxLoaded {
			treeInfoRow(c, locales.TreeShownFirst(len(objs), total), depth, t.TextMuted)
			return
		}
		label := locales.TreeLoadMore(len(objs), total)
		row := treeRowBase(c, label).PaddingStart(float32(4 + depth*folderIndent))
		row.Children(func() {
			ui.Text(c, label).SingleLine()
			ui.Spacer(c)
		})
		if row.Clicked() {
			a.loadObjectPage(p, schema, kind, false)
		}
	}
}

// previewObject 双击对象预览数据：优先复用该数据源已有的控制台，
// 没有控制台时才新建，避免堆出大量只有一句 SQL 的标签。
//
//   - 当前激活的就是该数据源的控制台：直接在它的光标处追加；
//   - 该数据源已有控制台：追加到第一个控制台保存的光标处并跳过去；
//   - 一个都没有：新建控制台。
//
// 只插入 SQL，不自动执行。
func (a *root) previewObject(p app.ConnProfile, o engine.DBObject) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	a.connectAsync(p, func(err error) {
		if err != nil {
			a.consoleErr = err.Error()
			return
		}
		a.previewObjectNow(p, o, m)
	})
}

// previewObjectNow 是 previewObject 建连之后的部分：只插 SQL，不自动执行。
func (a *root) previewObjectNow(p app.ConnProfile, o engine.DBObject, m *app.ConsoleManager) {
	sql := "SELECT * FROM " + qualifiedName(p.Kind, o) + " LIMIT 200;"
	if recs := m.ConsolesOf(p.ID); len(recs) > 0 {
		target := recs[0]
		// 当前激活的控制台已经属于这个数据源时，优先留在原地。
		if cur, ok := a.store.GetConsole(a.activeConsole); ok && cur.ConnectionID == p.ID {
			target = cur
		}
		a.pendingActivate = target.ID
		a.pendingAppend = sql
		return
	}
	rec, err := m.Create(p.ID, locales.TreePreviewConsole(o.Name))
	if err != nil {
		a.consoleErr = locales.TreePreviewFailed(err.Error())
		return
	}
	a.pendingActivate = rec.ID
	a.pendingAppend = sql
}

// showDDL 取对象的 DDL 并弹窗（headless 同步，有窗口时后台 + Update）。
func (a *root) showDDL(p app.ConnProfile, o engine.DBObject) {
	a.connectAsync(p, func(err error) {
		if err != nil {
			a.consoleErr = err.Error()
			return
		}
		a.loadObjectDDL(p, o)
	})
}

// fetchObjectDDL 读取对象 DDL：supported=false 表示驱动不支持该能力。
// Ctrl+Q 弹窗与表视图 DDL 页共用，保证两处内容一致。
func fetchObjectDDL(ctx context.Context, sess engine.Session, o engine.DBObject) (string, bool, error) {
	if sess == nil {
		return "", false, nil
	}
	lister, hasDDL := sess.(engine.ObjectDDLer)
	scoped, hasScoped := sess.(engine.SchemaObjectDDLer)
	if !sess.Caps().Has(engine.CapObjectDDL) || (!hasDDL && !hasScoped) {
		return "", false, nil
	}
	if hasScoped {
		ddl, err := scoped.ObjectDDLIn(ctx, o.Schema, o.Kind, o.Name)
		return ddl, true, err
	}
	ddl, err := lister.ObjectDDL(ctx, o.Kind, o.Name)
	return ddl, true, err
}

// loadObjectDDL 在会话就绪后读 DDL；headless 同步，有窗口时后台 + Update。
func (a *root) loadObjectDDL(p app.ConnProfile, o engine.DBObject) {
	sess := a.sessionFor(p.ID)
	load := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ddl, supported, err := fetchObjectDDL(ctx, sess, o)
		if !supported {
			a.setDDLNote(o.Name, locales.TreeDDLUnsupported())
			return
		}
		apply := func() {
			if err != nil {
				a.setDDLNote(o.Name, locales.TreeDDLFailed(err.Error()))
				app.LogWarn("ddl", "load failed", "object", o.Name, "err", err.Error())
				return
			}
			a.setDDL(o.Name, ddl)
		}
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, apply)
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

// ddlConnForCaret 找出「编辑器光标查 DDL」要用的连接：
// 优先激活控制台所属的连接（用户多半正对着控制台），其次左侧树选中的连接。
func (a *root) ddlConnForCaret() (app.ConnProfile, bool) {
	if id := a.activeConnID(); id != "" && a.store != nil {
		if p, ok := a.store.Get(id); ok {
			return p, true
		}
	}
	if a.selConn.ID != "" {
		return a.selConn, true
	}
	return app.ConnProfile{}, false
}

// viewDDLFromEditor 尝试「按 Ctrl+Q 时查光标处的表」：成功返回 true。
// 失败时不弹任何东西（左侧树选中对象那条路还能走）。
func (a *root) viewDDLFromEditor() bool {
	return a.viewDDLForWord(editorWordAtCaret(a.code))
}

// viewDDLForWord 查一个标识符的 DDL：Ctrl+Q（光标处）与编辑器 Ctrl+点击共用同一条路径，
// 包括「只认已知表/视图」的过滤。空词或不成功时不弹任何东西。
func (a *root) viewDDLForWord(word string) bool {
	if word == "" {
		return false
	}
	p, ok := a.ddlConnForCaret()
	if !ok {
		return false
	}
	sess := a.sessionFor(p.ID)
	if sess == nil {
		return false
	}
	if _, ok := sess.(engine.ObjectDDLer); !ok || !sess.Caps().Has(engine.CapObjectDDL) {
		return false
	}
	// 对象树/补全目录已加载时先确认光标处的词确实是个表/视图，
	// 免得把 select / 列名也当表名去查、弹一个「未找到」出来。
	kind := engine.ObjectTable
	if known := a.knownObjects(p.ID); known != nil {
		k, ok := known[strings.ToLower(word)]
		if !ok {
			return false
		}
		kind = k
	}
	a.showDDL(p, engine.DBObject{Kind: kind, Name: word, Schema: a.connSchema[p.ID]})
	return true
}

// onEditorDefinition 是编辑器 Ctrl+点击 / F12 的回调：把指针下的标识符当表名查 DDL，
// 行为与光标停在该词上按 Ctrl+Q 一致；不是已知表/视图时什么也不做。
func (a *root) onEditorDefinition(offset int) {
	if a.code == nil {
		return
	}
	a.viewDDLForWord(editorWordAt(a.code, offset))
}

// knownObjects 返回该连接已知的表/视图名（小写）与类型；
// 树和补全目录都没加载时返回 nil。
func (a *root) knownObjects(connID string) map[string]engine.ObjectKind {
	key, _ := a.completionScope(connID)
	out := map[string]engine.ObjectKind{}
	if a.catalog != nil {
		if tables, views, ok := a.catalog.objects(key); ok {
			for _, n := range views {
				out[strings.ToLower(n)] = engine.ObjectView
			}
			for _, n := range tables {
				out[strings.ToLower(n)] = engine.ObjectTable
			}
		}
	}
	if m := a.treeData[key]; m != nil {
		for _, kind := range []engine.ObjectKind{engine.ObjectTable, engine.ObjectView} {
			for _, o := range m[kind] {
				out[strings.ToLower(o.Name)] = kind
			}
		}
	}
	if len(out) == 0 {
		return nil // 加载过但还空，当作未知
	}
	return out
}

// editorWordAtCaret 从编辑器的光标位置提取标识符（表名/列名）。
func editorWordAtCaret(c *codeEditor) string {
	text := c.Text()
	_, caretRune := c.CaretRunes()
	return editorWordAt(c, runeToByte(text, caretRune))
}

// editorWordAt 从字节偏移提取它所在的标识符；指针可能停在词的任意位置
// （甚至词中间），所以向前后各扫一次完整词边界。偏移越界时夹紧。
func editorWordAt(c *codeEditor, pos int) string {
	text := c.Text()
	if pos < 0 {
		pos = 0
	}
	if pos > len(text) {
		pos = len(text)
	}
	for pos > 0 && pos < len(text) && !utf8.RuneStart(text[pos]) {
		pos-- // 落在多字节字符中间时回到该字符起点
	}

	end := pos
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !isIdentRune(r) {
			break
		}
		end += size
	}
	start := pos
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !isIdentRune(r) {
			break
		}
		start -= size
	}
	if start == end {
		return ""
	}
	return text[start:end]
}

// isIdentRune 判断 rune 是否为 SQL 标识符合法字符（字母数字下划线）。
func isIdentRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' || r == '_'
}

// ddlViewState 返回 DDL 只读编辑器状态（首次用时创建）：SQL 高亮、可选中复制。
func (a *root) ddlViewState() *myeditor.State {
	if a.ddlState == nil {
		buf := myeditor.NewBuffer("")
		buf.SetLanguage(sqlLanguageForKind(a.connKind()))
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
		ui.Text(c, locales.TreeDDLTitle(a.ddlTitle)).Bold().SingleLine()
		myeditor.View(c, a.ddlViewState()).FillWidth().Height(ddlPanelHeight - 110)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.TreeCopyAll()).Tooltip(locales.TreeCopyAllTip()).
				Clicked() {
				c.WriteClipboard(a.ddlState.Buffer.String())
			}
			if ui.PrimaryButton(c, locales.CommonClose()).Clicked() {
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
