package ui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
	"db-g/sql"
)

// completionTimeout 限住一次目录枚举，避免慢库拖住补全。
const completionTimeout = 5 * time.Second

// onCompletion 是编辑器的补全回调：按光标上下文给候选（点号列 / 语句表列 / 目录 / 词表）。
func (a *root) onCompletion(r myeditor.CompletionRequest) {
	kind := a.connKindOf(a.activeConsoleConnID())
	// 注释里不做自动补全；显式 Ctrl+Space 仍然可以调出候选。
	if !r.Explicit && sqlInComment(a.code.Text(), r.Offset, kind) {
		a.code.state.ShowCompletions(-1, nil)
		return
	}
	a.completionWanted = true
	a.completionWordStart = r.Offset - len(r.Prefix)
	items := a.completionItemsAt(r.Offset)
	a.code.state.ShowCompletions(r.Offset-len(r.Prefix), items)
}

// sqlInComment 报告 offset 是否落在 SQL 注释（--、#、/* */）里。
func sqlInComment(text string, offset int, kind string) bool {
	inComment, _ := sqlLexAt(text, offset, kind)
	return inComment
}

// sqlInString 报告 offset 是否落在带引号的字面量或标识符里。
func sqlInString(text string, offset int, kind string) bool {
	_, inString := sqlLexAt(text, offset, kind)
	return inString
}

// sqlLexAt 判断 offset 是否落在注释或引号文本里。字符串与引号标识符里的
// 注释记号不算注释；# 仅 MySQL 支持。
func sqlLexAt(text string, offset int, kind string) (inComment, inString bool) {
	if offset > len(text) {
		offset = len(text)
	}
	hashComments := kind == "mysql"
	for i := 0; i < offset; {
		switch {
		case text[i] == '-' && i+1 < len(text) && text[i+1] == '-':
			end := len(text)
			if nl := strings.IndexByte(text[i:], '\n'); nl >= 0 {
				end = i + nl
			}
			if offset > i && offset <= end {
				return true, false
			}
			i = end
		case hashComments && text[i] == '#':
			end := len(text)
			if nl := strings.IndexByte(text[i:], '\n'); nl >= 0 {
				end = i + nl
			}
			if offset > i && offset <= end {
				return true, false
			}
			i = end
		case text[i] == '/' && i+1 < len(text) && text[i+1] == '*':
			end := len(text)
			if close := strings.Index(text[i+2:], "*/"); close >= 0 {
				end = i + 2 + close + 2
			}
			if offset > i && (end == len(text) || offset < end) {
				return true, false
			}
			i = end
		case text[i] == '\'' || text[i] == '"' || text[i] == '`':
			quote := text[i]
			open := i + 1
			i++
			closed := false
			for i < len(text) {
				if text[i] == '\\' && quote != '`' && i+1 < len(text) {
					i += 2
					continue
				}
				if text[i] != quote {
					i++
					continue
				}
				if i+1 < len(text) && text[i+1] == quote {
					i += 2 // 连写两个引号是转义
					continue
				}
				closed = true
				break
			}
			if !closed {
				return false, true
			}
			if offset <= i {
				return false, offset >= open
			}
			i++ // 引号后面可能还有注释，继续扫
		default:
			i++
		}
	}
	return false, false
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
	key, _ := a.completionScope(connID)
	if items, ok := a.session.Catalog.get(key); ok {
		return items
	}
	return sqlWordItemsForKind(a.connKindOf(connID))
}

// ensureCatalog 在主线程调用：没有新鲜缓存时发起一次加载。
// 有窗口时后台取数、完成后回投重弹；headless（测试）同步执行便于断言。
func (a *root) ensureCatalog(connID string) {
	key, schema := a.completionScope(connID)
	kind := a.connKindOf(connID)
	if !a.session.Catalog.begin(key) {
		return
	}
	// 在主线程捕获会话，后台协程只用捕获到的接口值（不读会话表，避免数据竞争）。
	sess := a.sessionFor(connID)
	if sess == nil {
		a.session.Catalog.fail(key)
		return
	}
	load := func() {
		start := time.Now()
		tables, views, err := objectsOfIn(sess, schema)
		if err != nil {
			a.session.Catalog.fail(key)
			app.LogWarn("catalog", "tables load failed", "conn", connID, "schema", schema, "err", err.Error())
			return
		}
		a.session.Catalog.finish(key, tables, views, kind)
		app.LogInfo("catalog", "tables loaded", "conn", connID, "schema", schema, "tables", len(tables), "views", len(views), "dur_ms", time.Since(start).Milliseconds())
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, a.retriggerCompletions)
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
	key, schema := a.completionScope(connID)
	a.ensureColumnsKey(key, connID, schema, table)
}

// catalogKey 是某数据源某个 schema 的目录/列缓存键（与 completionScope 一致）。
func catalogKey(connID, schema string) string {
	if schema == "" {
		return connID
	}
	return connID + "|" + schema
}

// ensureColumnsKey 是 ensureColumns 的显式 schema 版本：表视图按对象所属 schema 读列，
// 不依赖顶栏当前选的 schema。
func (a *root) ensureColumnsKey(key, connID, schema, table string) {
	kind := a.connKindOf(connID)
	if table == "" || !a.session.Catalog.beginColumns(key, table) {
		return
	}
	sess := a.sessionFor(connID)
	if sess == nil || !sess.Caps().Has(engine.CapColumns) {
		a.session.Catalog.failColumns(key, table)
		return
	}
	load := func() {
		start := time.Now()
		cols, err := columnsOf(sess, schema, table)
		if err != nil {
			a.session.Catalog.failColumns(key, table)
			app.LogWarn("catalog", "columns load failed", "conn", connID, "schema", schema, "table", table, "err", err.Error())
			return
		}
		a.session.Catalog.finishColumns(key, table, cols, kind)
		app.LogInfo("catalog", "columns loaded", "conn", connID, "schema", schema, "table", table, "cols", len(cols), "dur_ms", time.Since(start).Milliseconds())
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, func() {
				if a.completionStillWanted() {
					a.showCompletionsAtCursor()
				}
			})
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
func columnsOf(sess engine.Session, schema, table string) ([]engine.Column, error) {
	if !sess.Caps().Has(engine.CapColumns) {
		return nil, nil // 能力不足：降级为只有关键字/表
	}
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	if lister, ok := sess.(engine.SchemaColumnLister); ok {
		return lister.ColumnsIn(ctx, schema, table)
	}
	lister, ok := sess.(engine.ColumnLister)
	if !ok {
		return nil, nil // 能力不足：降级为只有关键字/表
	}
	return lister.Columns(ctx, table)
}

// catalogObjectLimit 是补全目录最多取的表/视图数量。大库（几万张表）不全量拉，
// 与 db-k 的元数据策略一致：只取第一页，避免后台构建几万条候选拖慢 UI。
const catalogObjectLimit = 2000

// objectsOf 用会话枚举默认 schema 的表与视图（纯函数，可被后台协程安全调用）。
func objectsOf(sess engine.Session) (tables, views []string, err error) {
	return objectsOfIn(sess, "")
}

// objectsOfIn 用会话枚举指定 schema/库的表与视图；schema 为空表示默认 schema。
func objectsOfIn(sess engine.Session, schema string) (tables, views []string, err error) {
	lister, ok := sess.(engine.ObjectLister)
	if !ok || !sess.Caps().Has(engine.CapObjects) {
		return nil, nil, nil // 能力不足：降级为只有关键字
	}
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	pager, _ := sess.(engine.PagedObjectLister)
	scoped, multi := sess.(engine.SchemaLister)
	names := func(kind engine.ObjectKind) ([]string, error) {
		var objs []engine.DBObject
		if pager != nil {
			objs, err = pager.ObjectsPage(ctx, schema, kind, 0, catalogObjectLimit)
		} else if schema != "" && multi {
			objs, err = scoped.ObjectsIn(ctx, schema, kind)
			if len(objs) > catalogObjectLimit {
				objs = objs[:catalogObjectLimit]
			}
		} else {
			objs, err = lister.Objects(ctx, kind)
			if len(objs) > catalogObjectLimit {
				objs = objs[:catalogObjectLimit]
			}
		}
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
	sess := a.sessionFor(connID)
	if sess == nil {
		return nil, nil, errors.New(locales.CompletionConnClosed())
	}
	_, schema := a.completionScope(connID)
	return objectsOfIn(sess, schema)
}

// completionItemsAt 计算光标 offset 处的候选，并顺手发起缺失的加载。
//
// 优先级：点号列（表名/别名） → 语句里引用的表的列 → 目录（表/视图+词） → 词表。
// 加载在 headless 下是同步的，所以最多重算 3 轮（目录 → 列 → 完整），
// 真机上第二轮通常仍是部分结果，等后台加载完成由 retrigger 再补。
func (a *root) completionItemsAt(offset int) []myeditor.Completion {
	if a.computingCompletion {
		return sqlWordItemsForKind(a.connKindOf(a.activeConsoleConnID()))
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
// 只有用户还在补全那个词时才真的弹；否则后台加载完成会凭空弹出补全浮窗
// （长查询结束 → 结果区判定可编辑 → 加载列 → 回投，就是这条路径）。
func (a *root) requestRetrigger() {
	if a.computingCompletion {
		a.retriggerPending = true
		return
	}
	if !a.completionStillWanted() {
		return
	}
	a.showCompletionsAtCursor()
}

// completionStillWanted 报告后台加载完成时是否还该刷新补全弹层：编辑区仍有焦点，
// 且光标还停在请求补全的那个词里（点走、换行或换到别的词就不再弹）。
func (a *root) completionStillWanted() bool {
	if !a.completionWanted || !a.code.state.Focused() {
		return false
	}
	text := a.code.Text()
	off := a.code.state.Cursor()
	if off < a.completionWordStart || off > len(text) {
		return false
	}
	for _, r := range text[a.completionWordStart:off] {
		if !completionWordRune(r) {
			return false
		}
	}
	return completionWordStart(text, off) == a.completionWordStart
}

func completionWordRune(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// completionWordStart 返回光标所在词的起点（字节偏移）。
func completionWordStart(text string, off int) int {
	off = min(off, len(text))
	for off > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:off])
		if !completionWordRune(r) {
			break
		}
		off -= size
	}
	return off
}

// computeCompletion 是一轮候选计算；more 为 true 表示本次发起了加载、值得再算一轮。
func (a *root) computeCompletion(offset int) (items []myeditor.Completion, more bool) {
	text := a.code.Text()
	connID := a.activeConsoleConnID()
	key, _ := a.completionScope(connID)
	kind := a.connKindOf(connID)
	scope, scopeOff := sql.StatementScope(text, offset)
	rels := sql.Relations(scope)
	aliases := sql.AliasMap(rels)

	// 1) 点号上下文：`表名.` / `别名.`
	if ident, ok := dotIdent(text, offset); ok {
		table := ident
		if t, isAlias := aliases[ident]; isAlias {
			table = t
		}
		if table != "" && a.session.Catalog.hasRelation(key, table) {
			if cols, hit := a.session.Catalog.getColumns(key, table); hit {
				return cols, false
			}
			a.ensureColumns(connID, table)
			return sqlWordItemsForKind(kind), true
		}
		if !a.catalogLoaded(connID) {
			a.ensureCatalog(connID)
			return sqlWordItemsForKind(kind), true
		}
	}

	// 2) 关系名位置（FROM/JOIN 列表、UPDATE 目标、INSERT INTO）：优先给表/视图。
	// 即使上一条语句没写分号、后续 SELECT 被并进同一作用域，也只按表名补全。
	// 放在点号之后：`表名.` 仍然优先补列（与既有行为一致）。
	if sql.RelationPositionAt(scope, scopeOff) {
		if items, ok := a.session.Catalog.get(key); ok {
			return items, false
		}
		a.ensureCatalog(connID)
		return sqlWordItemsForKind(kind), true
	}

	// 2) 语句里引用的表：补它们的列（多表 JOIN 时全部）。
	if tables := a.knownRelations(connID, rels); len(tables) > 0 {
		cols, all := a.session.Catalog.getColumnsMany(key, tables)
		if !all {
			missing := false
			for i, t := range tables {
				if cols[i] == nil {
					missing = true
					a.ensureColumns(connID, t)
				}
			}
			cols, _ = a.session.Catalog.getColumnsMany(key, tables)
			return buildStatementItems(tables, cols, kind), missing
		}
		return buildStatementItems(tables, cols, kind), false
	}

	// 3) 目录（表/视图 + 词）或纯词表；语句里有表名但目录还没加载时先加载目录。
	if items, ok := a.session.Catalog.get(key); ok {
		return items, false
	}
	a.ensureCatalog(connID)
	return sqlWordItemsForKind(kind), true // 目录加载后重算一轮（headless 同步，真机等回调）
}

// catalogLoaded 报告该连接当前 schema 的表/视图目录是否已有新鲜缓存。
func (a *root) catalogLoaded(connID string) bool {
	key, _ := a.completionScope(connID)
	_, ok := a.session.Catalog.get(key)
	return ok
}

// knownRelations 过滤出目录里认识的关系（大小写不敏感），去重保序，最多 8 张。
func (a *root) knownRelations(connID string, rels []sql.Relation) []string {
	scopeKey, _ := a.completionScope(connID)
	out := make([]string, 0, len(rels))
	seen := map[string]bool{}
	for _, r := range rels {
		if r.Table == "" {
			continue
		}
		key := strings.ToLower(r.Table)
		if seen[key] || !a.session.Catalog.hasRelation(scopeKey, r.Table) {
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
	kind := a.connKindOf(a.activeConsoleConnID())
	if sqlInComment(a.code.Text(), a.code.state.Cursor(), kind) {
		a.code.state.ShowCompletions(-1, nil)
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
	a.session.Catalog.invalidate(connID)
	a.ensureCatalog(connID)
	a.updateSettingsWindow()
}
