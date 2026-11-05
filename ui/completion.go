package ui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
)

// completionTimeout 限住一次目录枚举，避免慢库拖住补全。
const completionTimeout = 5 * time.Second

// onCompletion 是编辑器的补全回调：按光标上下文给候选（点号列 / 语句表列 / 目录 / 词表）。
func (a *root) onCompletion(r myeditor.CompletionRequest) {
	items := a.completionItemsAt(r.Offset)
	a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items)
}

// activeConsoleConnID 返回当前控制台的数据源 id（无控制台为空）。
func (a *root) activeConsoleConnID() string {
	if m := a.consoleMgr(); m != nil {
		if rec, ok := m.Active(); ok {
			return rec.ConnectionID
		}
	}
	return ""
}

// completionItems 返回某连接的候选：缓存命中用组装好的，否则只有词表。
func (a *root) completionItems(connID string) []myeditor.Completion {
	if items, ok := a.catalog.get(connID); ok {
		return items
	}
	return sqlWordItems()
}

// ensureCatalog 在主线程调用：没有新鲜缓存时发起一次加载。
// 有窗口时后台取数、完成后回投重弹；headless（测试）同步执行便于断言。
func (a *root) ensureCatalog(connID string) {
	if connID == "" || !a.catalog.begin(connID) {
		return
	}
	// 在主线程捕获会话，后台协程只用捕获到的接口值（不读 a.sess，避免数据竞争）。
	sess := a.sess
	if sess == nil || a.sessConnID != connID {
		a.catalog.fail(connID)
		return
	}
	load := func() {
		start := time.Now()
		tables, views, err := objectsOf(sess)
		if err != nil {
			a.catalog.fail(connID)
			app.LogWarn("catalog", "tables load failed", "conn", connID, "err", err.Error())
			return
		}
		a.catalog.finish(connID, tables, views)
		app.LogInfo("catalog", "tables loaded", "conn", connID, "tables", len(tables), "views", len(views), "dur_ms", time.Since(start).Milliseconds())
		if a.win != nil {
			win := a.win
			win.Update(a.retriggerCompletions)
		}
	}
	if a.win == nil {
		load()
		a.requestRetrigger()
		return
	}
	go load()
}

// ensureColumns 在主线程调用：没有新鲜列缓存时发起一次加载（同 ensureCatalog 的线程语义）。
func (a *root) ensureColumns(connID, table string) {
	if connID == "" || table == "" || !a.catalog.beginColumns(connID, table) {
		return
	}
	sess := a.sess
	if sess == nil || a.sessConnID != connID || !sess.Caps().Has(engine.CapColumns) {
		a.catalog.failColumns(connID, table)
		return
	}
	load := func() {
		start := time.Now()
		cols, err := columnsOf(sess, table)
		if err != nil {
			a.catalog.failColumns(connID, table)
			app.LogWarn("catalog", "columns load failed", "conn", connID, "table", table, "err", err.Error())
			return
		}
		a.catalog.finishColumns(connID, table, cols)
		app.LogInfo("catalog", "columns loaded", "conn", connID, "table", table, "cols", len(cols), "dur_ms", time.Since(start).Milliseconds())
		if a.win != nil {
			win := a.win
			win.Update(a.showCompletionsAtCursor)
		}
	}
	if a.win == nil {
		load()
		a.requestRetrigger()
		return
	}
	go load()
}

// columnsOf 用会话列出某表的列（纯函数，可被后台协程安全调用）。
func columnsOf(sess engine.Session, table string) ([]engine.Column, error) {
	lister, ok := sess.(engine.ColumnLister)
	if !ok || !sess.Caps().Has(engine.CapColumns) {
		return nil, nil // 能力不足：降级为只有关键字/表
	}
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	return lister.Columns(ctx, table)
}

// objectsOf 用会话枚举表与视图（纯函数，可被后台协程安全调用）。
func objectsOf(sess engine.Session) (tables, views []string, err error) {
	lister, ok := sess.(engine.ObjectLister)
	if !ok || !sess.Caps().Has(engine.CapObjects) {
		return nil, nil, nil // 能力不足：降级为只有关键字
	}
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	names := func(kind engine.ObjectKind) ([]string, error) {
		objs, err := lister.Objects(ctx, kind)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			if o.Name != "" {
				out = append(out, o.Name)
			}
		}
		sort.Strings(out)
		return out, nil
	}
	if tables, err = names(engine.ObjectTable); err != nil {
		return nil, nil, err
	}
	if views, err = names(engine.ObjectView); err != nil {
		return nil, nil, err
	}
	return tables, views, nil
}

// loadCatalog 在主线程取一次目录（测试与刷新用）。
func (a *root) loadCatalog(connID string) (tables, views []string, err error) {
	if a.sess == nil || a.sessConnID != connID {
		return nil, nil, errors.New("连接未打开")
	}
	return objectsOf(a.sess)
}

// completionItemsAt 计算光标 offset 处的候选，并顺手发起缺失的加载。
//
// 优先级：点号列（表名/别名） → 语句里引用的表的列 → 目录（表/视图+词） → 词表。
// 加载在 headless 下是同步的，所以最多重算 3 轮（目录 → 列 → 完整），
// 真机上第二轮通常仍是部分结果，等后台加载完成由 retrigger 再补。
func (a *root) completionItemsAt(offset int) []myeditor.Completion {
	if a.computingCompletion {
		return sqlWordItems()
	}
	a.computingCompletion = true
	defer func() { a.computingCompletion = false }()

	var items []myeditor.Completion
	for pass := 0; pass < 3; pass++ {
		var more bool
		items, more = a.computeCompletion(offset)
		if !more {
			break
		}
	}
	a.computingCompletion = false
	// 计算期间完成的同步加载（headless）在这里补一次重弹，拿到完整候选。
	if a.retriggerPending {
		a.retriggerPending = false
		a.showCompletionsAtCursor()
	}
	return items
}

// requestRetrigger 在加载完成后请求重弹；若正在计算候选则作业推迟到计算结束。
func (a *root) requestRetrigger() {
	if a.computingCompletion {
		a.retriggerPending = true
		return
	}
	a.showCompletionsAtCursor()
}

// computeCompletion 是一轮候选计算；more 为 true 表示本次发起了加载、值得再算一轮。
func (a *root) computeCompletion(offset int) (items []myeditor.Completion, more bool) {
	text := a.code.Text()
	connID := a.activeConsoleConnID()
	rels := relationsIn(stmtScopeAt(text, offset))
	aliases := aliasMap(rels)

	// 1) 点号上下文：`表名.` / `别名.`
	if ident, ok := dotIdent(text, offset); ok {
		table := ident
		if t, isAlias := aliases[ident]; isAlias {
			table = t
		}
		if table != "" && a.catalog.hasRelation(connID, table) {
			if cols, hit := a.catalog.getColumns(connID, table); hit {
				return cols, false
			}
			a.ensureColumns(connID, table)
			return sqlWordItems(), true
		}
		if !a.catalogLoaded(connID) {
			a.ensureCatalog(connID)
			return sqlWordItems(), true
		}
	}

	// 2) 语句里引用的表：补它们的列（多表 JOIN 时全部）。
	if tables := a.knownRelations(connID, rels); len(tables) > 0 {
		cols, all := a.catalog.getColumnsMany(connID, tables)
		if !all {
			missing := false
			for i, t := range tables {
				if cols[i] == nil {
					missing = true
					a.ensureColumns(connID, t)
				}
			}
			cols, _ = a.catalog.getColumnsMany(connID, tables)
			return buildStatementItems(tables, cols), missing
		}
		return buildStatementItems(tables, cols), false
	}

	// 3) 目录（表/视图 + 词）或纯词表；语句里有表名但目录还没加载时先加载目录。
	if items, ok := a.catalog.get(connID); ok {
		return items, false
	}
	a.ensureCatalog(connID)
	return sqlWordItems(), true // 目录加载后重算一轮（headless 同步，真机等回调）
}

// catalogLoaded 报告该连接的表/视图目录是否已有新鲜缓存。
func (a *root) catalogLoaded(connID string) bool {
	_, ok := a.catalog.get(connID)
	return ok
}

// knownRelations 过滤出目录里认识的关系（大小写不敏感），去重保序，最多 8 张。
func (a *root) knownRelations(connID string, rels []relationRef) []string {
	out := make([]string, 0, len(rels))
	seen := map[string]bool{}
	for _, r := range rels {
		if r.Table == "" {
			continue
		}
		key := strings.ToLower(r.Table)
		if seen[key] || !a.catalog.hasRelation(connID, r.Table) {
			continue
		}
		seen[key] = true
		out = append(out, r.Table)
		if len(out) == 8 {
			break
		}
	}
	return out
}

// retriggerCompletions 在目录/列就绪后把候选补上。
func (a *root) retriggerCompletions() { a.requestRetrigger() }

// showCompletionsAtCursor 重算当前上下文（点号列 / 目录 / 词表）并刷新弹层。
// 不能用 Run(ShowCompletions)：编辑器在同一词起点已有弹层时会短路成「本地重过滤」，
// 不会再问一次 onCompletion（目录就永远补不进去）。
func (a *root) showCompletionsAtCursor() {
	if a.win == nil && !a.code.state.Focused() {
		return
	}
	a.code.state.ShowCompletions(-1, a.completionItemsAt(a.code.state.Cursor()))
}

// refreshCompletions 失效当前连接的缓存并重新加载（设置窗口按钮）。
func (a *root) refreshCompletions() {
	connID := a.activeConsoleConnID()
	if connID == "" {
		return
	}
	a.catalog.invalidate(connID)
	a.ensureCatalog(connID)
	a.updateSettingsWindow()
}
