package ui

import (
	"context"
	"errors"
	"sort"
	"time"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/engine"
)

// completionTimeout 限住一次目录枚举，避免慢库拖住补全。
const completionTimeout = 5 * time.Second

// onCompletion 是编辑器的补全回调：缓存命中就直接给候选（含表/视图），
// 否则先给词表并触发一次后台加载。
func (a *root) onCompletion(r myeditor.CompletionRequest) {
	connID := a.activeConsoleConnID()
	// 点号上下文：`表名.` 之后只补该表的列（+ 关键字）。
	if ident, ok := dotIdent(a.code.Text(), r.Offset); ok && a.catalog.hasRelation(connID, ident) {
		if items, hit := a.catalog.getColumns(connID, ident); hit {
			a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items)
			return
		}
		a.code.state.ShowCompletions(r.Offset-len(r.Prefix), sqlWordItems())
		a.ensureColumns(connID, ident)
		return
	}
	if items, ok := a.catalog.get(connID); ok {
		a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items)
		return
	}
	a.code.state.ShowCompletions(r.Offset-len(r.Prefix), sqlWordItems())
	a.ensureCatalog(connID)
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
		tables, views, err := objectsOf(sess)
		if err != nil {
			a.catalog.fail(connID)
			return
		}
		a.catalog.finish(connID, tables, views)
		if a.win != nil {
			win := a.win
			win.Update(a.retriggerCompletions)
		}
	}
	if a.win == nil {
		load()
		a.retriggerCompletions()
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
		cols, err := columnsOf(sess, table)
		if err != nil {
			a.catalog.failColumns(connID, table)
			return
		}
		a.catalog.finishColumns(connID, table, cols)
		if a.win != nil {
			win := a.win
			win.Update(a.showCompletionsAtCursor)
		}
	}
	if a.win == nil {
		load()
		a.showCompletionsAtCursor()
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

// retriggerCompletions 在目录/列就绪后把候选补上。
func (a *root) retriggerCompletions() { a.showCompletionsAtCursor() }

// showCompletionsAtCursor 重算当前上下文（点号列 / 目录 / 词表）并刷新弹层。
// 不能用 Run(ShowCompletions)：编辑器在同一词起点已有弹层时会短路成「本地重过滤」，
// 不会再问一次 onCompletion（目录就永远补不进去）。
func (a *root) showCompletionsAtCursor() {
	if a.win == nil && !a.code.state.Focused() {
		return
	}
	connID := a.activeConsoleConnID()
	if ident, ok := dotIdent(a.code.Text(), a.code.state.Cursor()); ok {
		if items, hit := a.catalog.getColumns(connID, ident); hit {
			a.code.state.ShowCompletions(-1, items)
			return
		}
	}
	if items, ok := a.catalog.get(connID); ok {
		a.code.state.ShowCompletions(-1, items)
		return
	}
	a.code.state.ShowCompletions(-1, sqlWordItems())
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
