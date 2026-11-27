package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

func (a *root) openSQLite(path string) error {
	return a.openConn(app.ConnProfile{Name: filepath.Base(path), Kind: "sqlite", Path: path})
}

// engineProfile 把连接档案翻成引擎的 Profile：档案多出来的协议和连接参数在这里
// 一并带上，三处打开连接（正常打开/重连/测试连接）都用它。
func engineProfile(p app.ConnProfile) engine.Profile {
	return engine.Profile{
		Kind: p.Kind, Name: p.Name, DSN: p.Path,
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database,
		Protocol: p.Protocol, Params: p.Params,
	}
}

// openConn 连上（或复用）一个数据源，供 UI 各处调用；失败时圆点会变红。
func (a *root) openConn(p app.ConnProfile) error {
	return a.connect(p)
}

func (a *root) removeConn(id string) {
	if a.store == nil {
		return
	}
	app.LogInfo("conn", "remove", "id", id)
	var recs []app.ConsoleRecord
	if m := a.consoleMgr(); m != nil {
		recs = m.ConsolesOf(id)
	}
	if a.session.Catalog != nil {
		a.session.Catalog.invalidate(id)
	}
	a.forgetAIDDL(id)
	a.forgetAIRelations(id)
	a.forgetConnMeta(id)
	a.closeViewersOf(id)
	a.closeSession(id, "connection removed")
	if a.connErrOf(id) != "" {
		delete(a.session.Err, id)
	}
	if act, ok := a.store.Active(); ok && act.ID == id {
		a.store.SetActive("")
	}
	_ = a.store.Remove(id)
	if m := a.consoleMgr(); m != nil {
		m.OnConnectionDeleted(id, recs)
		for _, rec := range recs {
			delete(a.tabs.Consoles, rec.ID)
			delete(a.tabs.LastVer, rec.ID)
			a.code.Drop(rec.ID)
			if a.tabs.ActiveConsole == rec.ID {
				a.clearEditorConsole()
			}
		}
	}
	a.dropStmtRunsOf(idsOf(recs))
}

// stmtRun 是一个结果 tab：本次执行的语句 + 已钉住的历史结果。
// 顺序约定与 db-k 一致：pinned 始终在左侧，非 pinned 在右侧。
type stmtRun struct {
	stmt      app.SQLStatement
	sql       string // 执行时的语句快照：正文被编辑后偏移失效，执行/历史/行底色都只认它
	runtimeID string // 运行态 id；pin 后换成独立 id，结果不被同控制台的新执行覆盖
	started   bool
	pinned    bool // 钉住（仅内存态，重启消失）
	tabNo     int  // 结果编号（结果 N）：产生时分配，pin/取消/重排都不变
}

// pinMax 是单个控制台最多同时钉住的结果数（内存态上限，参照 db-k）。
const pinMax = 10

// rekeyRuntime 把某个运行态整体搬到新 id（pin 时用独立 id 保住结果快照）。
func (a *root) rekeyRuntime(oldID, newID string) {
	if oldID == newID {
		return
	}
	if c, ok := a.tabs.Consoles[oldID]; ok {
		a.tabs.Consoles[newID] = c
		delete(a.tabs.Consoles, oldID)
	}
	if ps, ok := a.tabs.PageStates[oldID]; ok {
		if ps.PageRuntime == oldID+"#page" {
			ps.PageRuntime = newID + "#page"
		}
		if ps.CountRuntime == oldID+"#count" {
			ps.CountRuntime = newID + "#count"
		}
		a.tabs.PageStates[newID] = ps
		delete(a.tabs.PageStates, oldID)
	}
	for _, suffix := range []string{"#page", "#count"} {
		if c, ok := a.tabs.Consoles[oldID+suffix]; ok {
			a.tabs.Consoles[newID+suffix] = c
			delete(a.tabs.Consoles, oldID+suffix)
		}
	}
	if es, ok := a.edit.Sets[oldID]; ok {
		a.edit.Sets[newID] = es
		delete(a.edit.Sets, oldID)
	}
}

// togglePin 切换当前结果 tab 的钉住状态（仅查询结果）。钉住后立即移到 pin 区最右并选中；
// 新执行时 pinned 结果保留在最前，新结果追加到右侧。交互与 db-k 的 togglePin 一致。
func (a *root) togglePin() {
	if len(a.tabs.StmtRuns) == 0 || a.tabs.ActiveStmt < 0 || a.tabs.ActiveStmt >= len(a.tabs.StmtRuns) {
		return
	}
	// 执行中禁止重排：stmtDone 按索引推进下一条，改顺序会执行错语句。
	for _, r := range a.tabs.StmtRuns {
		if a.runtimeFor(r.runtimeID).Running() {
			return
		}
	}
	cur := a.tabs.StmtRuns[a.tabs.ActiveStmt]
	rt := a.runtimeFor(cur.runtimeID)
	res := rt.Result()
	if res == nil || !res.IsQuery {
		return // 只允许钉住查询结果
	}
	pinnedCount := 0
	for _, r := range a.tabs.StmtRuns {
		if r.pinned {
			pinnedCount++
		}
	}
	rest := make([]stmtRun, 0, len(a.tabs.StmtRuns)-1)
	rest = append(rest, a.tabs.StmtRuns[:a.tabs.ActiveStmt]...)
	rest = append(rest, a.tabs.StmtRuns[a.tabs.ActiveStmt+1:]...)
	if cur.pinned {
		// 取消钉住：落到 pin 区之后的第一个位置（rest 里还剩 pinnedCount-1 个 pinned）。
		cur.pinned = false
		at := pinnedCount - 1
		a.tabs.StmtRuns = slices.Insert(rest, at, cur)
		a.tabs.ActiveStmt = at
	} else {
		if pinnedCount >= pinMax {
			return
		}
		// 换独立运行态 id：结果快照从此不再被同控制台的新执行覆盖。
		newID := fmt.Sprintf("%s#pin%d", a.tabs.ActiveConsole, cur.tabNo)
		a.rekeyRuntime(cur.runtimeID, newID)
		cur.runtimeID = newID
		cur.pinned = true
		a.tabs.StmtRuns = slices.Insert(rest, pinnedCount, cur)
		a.tabs.ActiveStmt = pinnedCount
	}
	a.refreshStmtMarks()
}

// consoleRuns 是一个控制台最近一次的结果 tab 与当前选中项（切控制台各自保留，参照 db-k）。
type consoleRuns struct {
	runs   []stmtRun
	active int
}

// saveStmtRuns 把当前控制台的结果 tab 存进按控制台的表（无控制台时丢弃）。
func (a *root) saveStmtRuns(consoleID string) {
	if consoleID == "" {
		return
	}
	if a.tabs.RunsByConsole == nil {
		a.tabs.RunsByConsole = map[string]consoleRuns{}
	}
	a.tabs.RunsByConsole[consoleID] = consoleRuns{runs: a.tabs.StmtRuns, active: a.tabs.ActiveStmt}
}

// loadStmtRuns 恢复某控制台上次的结果 tab；没有记录则清空。
func (a *root) loadStmtRuns(consoleID string) {
	if saved, ok := a.tabs.RunsByConsole[consoleID]; ok && len(saved.runs) > 0 {
		a.tabs.StmtRuns = saved.runs
		a.tabs.ActiveStmt = saved.active
		if a.tabs.ActiveStmt >= len(a.tabs.StmtRuns) {
			a.tabs.ActiveStmt = len(a.tabs.StmtRuns) - 1
		}
		return
	}
	a.tabs.StmtRuns, a.tabs.ActiveStmt = nil, 0
}

// idsOf 取出控制台 id 列表（给 dropStmtRunsOf 用）。
func idsOf(recs []app.ConsoleRecord) []string {
	out := make([]string, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.ID)
	}
	return out
}

// dropStmtRunsOf 移除指定控制台的结果 tab（含钉住的），并回收 pin 专用运行态。
func (a *root) dropStmtRunsOf(consoleIDs []string) {
	if len(a.tabs.StmtRuns) == 0 || len(consoleIDs) == 0 {
		return
	}
	owner := make(map[string]bool, len(consoleIDs))
	for _, id := range consoleIDs {
		owner[id] = true
	}
	baseID := func(id string) string {
		if i := strings.Index(id, "#"); i >= 0 {
			return id[:i]
		}
		return id
	}
	kept := make([]stmtRun, 0, len(a.tabs.StmtRuns))
	for _, run := range a.tabs.StmtRuns {
		if owner[baseID(run.runtimeID)] {
			a.discardEditState(run.runtimeID)
			a.cancelPageRuntimes(run.runtimeID)
			delete(a.tabs.Consoles, run.runtimeID)
			delete(a.tabs.PageStates, run.runtimeID)
			delete(a.tabs.Consoles, run.runtimeID+"#page")
			delete(a.tabs.Consoles, run.runtimeID+"#count")
			continue
		}
		kept = append(kept, run)
	}
	// 连没有结果 tab 的控制台编辑缓存也一起清掉（多语句的 id 是 console#i）。
	for id := range a.edit.Sets {
		if owner[baseID(id)] {
			delete(a.edit.Sets, id)
		}
	}
	a.tabs.StmtRuns = kept
	if a.tabs.ActiveStmt >= len(kept) {
		a.tabs.ActiveStmt = max(len(kept)-1, 0)
	}
	for _, id := range consoleIDs {
		delete(a.tabs.RunsByConsole, id)
	}
}

func (a *root) runSQL() {
	a.ai.Plan.open = false
	a.pollExternalFiles(true)
	if a.externalBlocked() {
		return
	}
	text := a.code.Text()
	ranges := a.execRanges(text)
	if len(ranges) == 0 {
		return
	}
	var risks []app.SQLRisk
	for _, st := range ranges {
		risks = append(risks, app.CheckSQLRisks(textSlice(text, st.Start, st.End))...)
	}
	if len(risks) > 0 {
		blocked := false
		if p, ok := a.store.Get(a.activeConnID()); ok {
			blocked = p.GuardBlock
		}
		a.modal.GuardOpen, a.modal.GuardBlocked, a.modal.GuardRisks = true, blocked, risks
		if blocked {
			a.consoleErr = locales.RunGuardBlocked(risks[0].Message)
			app.LogWarn("exec", "risky sql blocked", "rule", risks[0].Rule)
		}
		return
	}
	a.runSQLNow()
}

// runSQLNow 执行已经通过高危检查的语句。
func (a *root) runSQLNow() {
	ranges := a.execRanges(a.code.Text())
	if len(ranges) == 0 {
		return
	}
	// 有激活控制台时：先落盘，并确保连着它的数据源；没有控制台时沿用临时缓冲执行（不落盘）。
	if a.tabs.ActiveConsole != "" {
		if err := a.flushActive(); err != nil {
			a.externalFlushFailed(err)
			return
		}
		// 没连就后台连，连上后重新取一次语句再执行（等待期间正文可能已经改过）。
		a.ensureConsoleConnAsync(func() { a.startRuns(a.execRanges(a.code.Text())) })
		return
	}
	a.startRuns(ranges)
}

// startRuns 用给定语句重排结果 tab 并从当前位开始执行。
func (a *root) startRuns(ranges []app.SQLStatement) {
	if len(ranges) == 0 {
		return
	}
	a.resetStmtRuns(ranges)
	a.startStmt(a.tabs.ActiveStmt) // 跳过左侧已钉住的结果
}

// execRanges 算出本次要执行的语句：有选区 → 选区切分（偏移加上选区起点）；
// 否则 → 光标所在单条语句。
func (a *root) execRanges(text string) []app.SQLStatement {
	if sel, ok := a.code.SelectedText(); ok && strings.TrimSpace(sel) != "" {
		selStart, _, _ := a.code.Selection()
		out := make([]app.SQLStatement, 0, 4)
		for _, st := range app.SplitStatements(sel) {
			out = append(out, app.SQLStatement{Start: st.Start + selStart, End: st.End + selStart})
		}
		return out
	}
	if st, ok := app.StatementAt(text, a.code.state.Cursor()); ok {
		return []app.SQLStatement{st}
	}
	return nil
}

// truncateLogSQL 把日志里的 SQL 压到 200 个字符以内，截断时补 "..." 以示省略。
func truncateLogSQL(sql string) string {
	sql = strings.TrimSpace(sql)
	r := []rune(sql)
	if len(r) <= 200 {
		return sql
	}
	return string(r[:200]) + "..."
}

// textSlice 安全截取 [start,end)：偏移可能来自已被编辑的正文，越界时返回空串。
func textSlice(text string, start, end int) string {
	if start < 0 || start >= end || end > len(text) {
		return ""
	}
	return text[start:end]
}

// rerunnableSQL 返回当前结果对应的 SQL 快照；不是查询结果或没有快照时返回 false。
// 刷新按钮只在这种情况下出现：写语句/DDL 没有可重放的结果集。
func (a *root) rerunnableSQL() (string, bool) {
	if a.tabs.ActiveStmt < 0 || a.tabs.ActiveStmt >= len(a.tabs.StmtRuns) {
		return "", false
	}
	sql := strings.TrimSpace(a.tabs.StmtRuns[a.tabs.ActiveStmt].sql)
	if sql == "" {
		return "", false
	}
	if res := a.currentRuntime().Result(); res == nil || !res.IsQuery {
		return "", false
	}
	return sql, true
}

// anyRunRunning 报告当前控制台的结果 tab 里是否还有语句在跑（多语句顺序执行中）。
func (a *root) anyRunRunning() bool {
	for _, run := range a.tabs.StmtRuns {
		if a.runtimeFor(run.runtimeID).Running() {
			return true
		}
	}
	for _, ps := range a.tabs.PageStates {
		if ps.PageRuntime != "" && a.runtimeFor(ps.PageRuntime).Running() {
			return true
		}
		if ps.CountRuntime != "" && a.runtimeFor(ps.CountRuntime).Running() {
			return true
		}
	}
	return false
}

// rerunQuery 用当前结果对应的 SQL 在同一个结果 tab 里再执行一次（刷新）。
func (a *root) rerunQuery(sql string) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return
	}
	if a.anyRunRunning() {
		return
	}
	if a.tabs.ActiveConsole != "" {
		a.ensureConsoleConnAsync(func() { a.startRerun(sql) })
		return
	}
	a.startRerun(sql)
}

// startRerun 在会话就绪后执行刷新查询。
func (a *root) startRerun(sql string) {
	rt := a.currentRuntime()
	sess := a.activeSession()
	if sess == nil {
		a.consoleErr = locales.ResultConnectFirst()
		return
	}
	rtID := a.currentRuntimeID()
	rt.SetPageSize(a.settings.ResultPrefs.PageSize)
	a.tabs.LastExecSQL = sql
	app.LogInfo("exec", "rerun", "conn", a.activeConnName(), "console", a.tabs.ActiveConsole, "sql", truncateLogSQL(sql))
	rt.SetOnDone(a.rerunDone(rtID, sql))
	if !rt.RunTimeout(sess, sql, a.queryTimeoutFor()) {
		app.LogWarn("exec", "rerun not started", "console", a.tabs.ActiveConsole)
		return
	}
	a.discardEditState(rtID)
	a.refreshStmtMarks()
}

// rerunDone 是刷新执行的完成回调：记日志/历史、更新分页状态，但不推进下一条语句。
func (a *root) rerunDone(rtID, sql string) func(*engine.QueryResult, string, time.Duration) {
	logDone := a.execDoneLogger(rtID)
	connID, connName := a.activeConnID(), a.activeConnName()
	return func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		logDone(res, errMsg, dur)
		if a.history != nil {
			e := app.HistoryEntry{ConnID: connID, ConnName: connName, SQL: sql, DurMS: dur.Milliseconds(), OK: errMsg == "", Err: errMsg}
			if res != nil {
				e.Rows = len(res.Rows)
			}
			_ = a.history.Add(e)
		}
		rec := func() {
			a.recordPageState(rtID, sql, res)
			a.refreshStmtMarks()
		}
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, rec)
			return
		}
		rec()
	}
}

// cancelPageRuntimes 取消某个结果 tab 正在跑的翻页/统计任务。
func (a *root) cancelPageRuntimes(runtimeID string) {
	for _, suffix := range []string{"#page", "#count"} {
		if rt, ok := a.tabs.Consoles[runtimeID+suffix]; ok && rt != nil {
			rt.Cancel()
		}
	}
}

// resetStmtRuns 为本次执行准备运行时：单条语句沿用当前控制台的运行态（兼容既有行为），
// 多条语句各建一个（id = consoleID#i）。
func (a *root) resetStmtRuns(ranges []app.SQLStatement) {
	// 保留已钉住的结果在最前（左），本次新结果追加到右侧（对照 db-k）。
	pinned := make([]stmtRun, 0, len(a.tabs.StmtRuns))
	for _, run := range a.tabs.StmtRuns {
		if run.pinned {
			pinned = append(pinned, run)
			continue
		}
		// 丢弃的非 pinned 旧结果：连同编辑状态、分页缓存与 pin 专用运行态一起清理。
		a.discardEditState(run.runtimeID)
		a.cancelPageRuntimes(run.runtimeID)
		delete(a.tabs.PageStates, run.runtimeID)
		delete(a.tabs.Consoles, run.runtimeID+"#page")
		delete(a.tabs.Consoles, run.runtimeID+"#count")
		if strings.Contains(run.runtimeID, "#pin") {
			delete(a.tabs.Consoles, run.runtimeID)
		}
	}
	multi := len(ranges) > 1
	text := a.code.Text()
	runs := make([]stmtRun, 0, len(pinned)+len(ranges))
	runs = append(runs, pinned...)
	for i, st := range ranges {
		id := a.tabs.ActiveConsole
		if multi {
			id = fmt.Sprintf("%s#%d", a.tabs.ActiveConsole, i)
		}
		a.tabs.NextTabNo++
		runs = append(runs, stmtRun{stmt: st, sql: textSlice(text, st.Start, st.End), runtimeID: id, tabNo: a.tabs.NextTabNo})
	}
	a.tabs.StmtRuns = runs
	a.tabs.ActiveStmt = len(pinned)
	a.refreshStmtMarks()
}

// startStmt 执行第 i 条语句；完成回调里顺序推进下一条（失败即停）。
func (a *root) startStmt(i int) {
	if i < 0 || i >= len(a.tabs.StmtRuns) {
		return
	}
	a.tabs.StmtRuns[i].started = true
	a.tabs.ActiveStmt = i
	run := a.tabs.StmtRuns[i]
	sql := run.sql
	// 会话按运行态所属的控制台取，而不是「当前激活的数据源」：
	// 多语句执行途中切到表视图/别的数据源时，后续语句仍要跑在原数据源上。
	connID := a.runtimeConnID(run.runtimeID)
	sess := a.sessionFor(connID)
	if sess == nil {
		sess = a.activeSession() // 临时缓冲执行（无控制台）
	}
	app.LogInfo("exec", "start", "conn", a.connName(connID), "console", a.tabs.ActiveConsole, "sql", truncateLogSQL(sql))
	a.tabs.LastExecSQL = sql
	rt := a.runtimeFor(run.runtimeID)
	rt.SetPageSize(a.settings.ResultPrefs.PageSize)
	rt.SetOnDone(a.stmtDoneFor(run.runtimeID, sql, connID))
	if sess == nil {
		app.LogWarn("exec", "no session", "index", i)
		return
	}
	if !rt.RunTimeout(sess, sql, a.queryTimeoutForConn(connID)) {
		app.LogWarn("exec", "statement not started", "index", i)
	}
	a.refreshStmtMarks()
}

// queryTimeoutFor 返回当前执行数据源配置的查询超时；0 交给 Console 用默认 30 秒。
func (a *root) queryTimeoutFor() time.Duration {
	id := a.activeConnID()
	if a.store == nil || id == "" {
		return 0
	}
	p, ok := a.store.Get(id)
	if !ok || p.QueryTimeout <= 0 {
		return 0
	}
	return time.Duration(p.QueryTimeout) * time.Second
}

// runtimeConnID 返回运行态所属控制台的数据源 id（"consoleID#i"/"#page"/"#count"/"#pin" 都归 base）。
func (a *root) runtimeConnID(runtimeID string) string {
	base, _, _ := strings.Cut(runtimeID, "#")
	if id := a.consoleConnID(base); id != "" {
		return id
	}
	return ""
}

// queryTimeoutForConn 返回指定数据源的查询超时（0 = 默认）。
func (a *root) queryTimeoutForConn(connID string) time.Duration {
	if a.store == nil || connID == "" {
		return 0
	}
	p, ok := a.store.Get(connID)
	if !ok || p.QueryTimeout <= 0 {
		return 0
	}
	return time.Duration(p.QueryTimeout) * time.Second
}

// stmtDoneFor 是 stmtDone 的实现：connID 在语句启动时捕获，切标签不会把历史记到别的数据源。
func (a *root) stmtDoneFor(rtID, sql, connID string) func(*engine.QueryResult, string, time.Duration) {
	logDone := a.execDoneLogger(rtID)
	connName := a.connName(connID)
	return func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		logDone(res, errMsg, dur)
		if a.history != nil {
			e := app.HistoryEntry{ConnID: connID, ConnName: connName, SQL: sql, DurMS: dur.Milliseconds(), OK: errMsg == "", Err: errMsg}
			if res != nil {
				e.Rows = len(res.Rows)
			}
			_ = a.history.Add(e)
		}
		rec := func() { a.recordPageState(rtID, sql, res) }
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, rec)
		} else {
			rec()
		}
		ok := errMsg == ""
		advance := func() {
			a.refreshStmtMarks()
			if !ok {
				return
			}
			// DDL 成功后立即失效元数据缓存：否则补全/可编辑判定会继续用旧列。
			if app.IsSchemaChangeSQL(sql) {
				a.refreshConnMeta(connID)
			}
			// 找回该运行态当前所在位置，执行它的下一条（找不到说明结果已被清走）。
			for j, run := range a.tabs.StmtRuns {
				if run.runtimeID == rtID {
					a.startStmt(j + 1)
					return
				}
			}
		}
		if a.win != nil {
			win := a.win
			a.safeUpdate(win, advance)
			return
		}
		advance()
	}
}

// ensureConsoleConnAsync 保证已连接的控制台正是执行目标；没连就后台连上再调 then（UI 线程）。
// 连接条件不满足时直接写 consoleErr。
func (a *root) ensureConsoleConnAsync(then func()) {
	m := a.consoleMgr()
	if m == nil {
		a.consoleErr = locales.RunNoConsole()
		return
	}
	rec, ok := m.Active()
	if !ok {
		a.consoleErr = locales.RunNoActiveConsole()
		return
	}
	if a.connected(rec.ConnectionID) {
		a.touchSession(rec.ConnectionID)
		then()
		return
	}
	p, ok := a.store.Get(rec.ConnectionID)
	if !ok {
		a.consoleErr = locales.RunConnDeleted()
		return
	}
	a.connectAsync(p, func(err error) {
		if err != nil {
			a.consoleErr = err.Error()
			return
		}
		then()
	})
}
