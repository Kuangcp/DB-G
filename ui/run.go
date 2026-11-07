package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"db-g/app"
	"db-g/engine"
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
	err := a.connect(p)
	if err != nil {
		return err
	}
	a.connErr = ""
	return nil
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
	if a.catalog != nil {
		a.catalog.invalidate(id)
	}
	a.forgetConnMeta(id)
	a.closeSession(id, "connection removed")
	if a.connErrOf(id) != "" {
		delete(a.sessionErr, id)
		a.connErr = ""
	}
	if act, ok := a.store.Active(); ok && act.ID == id {
		a.store.SetActive("")
	}
	_ = a.store.Remove(id)
	if m := a.consoleMgr(); m != nil {
		m.OnConnectionDeleted(id, recs)
		for _, rec := range recs {
			delete(a.consoles, rec.ID)
			delete(a.lastVer, rec.ID)
			a.code.Drop(rec.ID)
			if a.activeConsole == rec.ID {
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
	if c, ok := a.consoles[oldID]; ok {
		a.consoles[newID] = c
		delete(a.consoles, oldID)
	}
	if fs, ok := a.fetchStates[oldID]; ok {
		a.fetchStates[newID] = fs
		delete(a.fetchStates, oldID)
	}
}

// togglePin 切换当前结果 tab 的钉住状态（仅查询结果）。钉住后立即移到 pin 区最右并选中；
// 新执行时 pinned 结果保留在最前，新结果追加到右侧。交互与 db-k 的 togglePin 一致。
func (a *root) togglePin() {
	if len(a.stmtRuns) == 0 || a.activeStmt < 0 || a.activeStmt >= len(a.stmtRuns) {
		return
	}
	// 执行中禁止重排：stmtDone 按索引推进下一条，改顺序会执行错语句。
	for _, r := range a.stmtRuns {
		if a.runtimeFor(r.runtimeID).Running() {
			return
		}
	}
	cur := a.stmtRuns[a.activeStmt]
	rt := a.runtimeFor(cur.runtimeID)
	res := rt.Result()
	if res == nil || !res.IsQuery {
		return // 只允许钉住查询结果
	}
	pinnedCount := 0
	for _, r := range a.stmtRuns {
		if r.pinned {
			pinnedCount++
		}
	}
	rest := make([]stmtRun, 0, len(a.stmtRuns)-1)
	rest = append(rest, a.stmtRuns[:a.activeStmt]...)
	rest = append(rest, a.stmtRuns[a.activeStmt+1:]...)
	if cur.pinned {
		// 取消钉住：落到 pin 区之后的第一个位置（rest 里还剩 pinnedCount-1 个 pinned）。
		cur.pinned = false
		at := pinnedCount - 1
		a.stmtRuns = slices.Insert(rest, at, cur)
		a.activeStmt = at
	} else {
		if pinnedCount >= pinMax {
			return
		}
		// 换独立运行态 id：结果快照从此不再被同控制台的新执行覆盖。
		newID := fmt.Sprintf("%s#pin%d", a.activeConsole, cur.tabNo)
		a.rekeyRuntime(cur.runtimeID, newID)
		cur.runtimeID = newID
		cur.pinned = true
		a.stmtRuns = slices.Insert(rest, pinnedCount, cur)
		a.activeStmt = pinnedCount
	}
	a.refreshStmtBackgrounds()
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
	if a.runsByConsole == nil {
		a.runsByConsole = map[string]consoleRuns{}
	}
	a.runsByConsole[consoleID] = consoleRuns{runs: a.stmtRuns, active: a.activeStmt}
}

// loadStmtRuns 恢复某控制台上次的结果 tab；没有记录则清空。
func (a *root) loadStmtRuns(consoleID string) {
	if saved, ok := a.runsByConsole[consoleID]; ok && len(saved.runs) > 0 {
		a.stmtRuns = saved.runs
		a.activeStmt = saved.active
		if a.activeStmt >= len(a.stmtRuns) {
			a.activeStmt = len(a.stmtRuns) - 1
		}
		a.refreshStmtBackgrounds()
		return
	}
	a.stmtRuns, a.activeStmt = nil, 0
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
	if len(a.stmtRuns) == 0 || len(consoleIDs) == 0 {
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
	kept := make([]stmtRun, 0, len(a.stmtRuns))
	for _, run := range a.stmtRuns {
		if owner[baseID(run.runtimeID)] {
			delete(a.consoles, run.runtimeID)
			delete(a.fetchStates, run.runtimeID)
			continue
		}
		kept = append(kept, run)
	}
	a.stmtRuns = kept
	if a.activeStmt >= len(kept) {
		a.activeStmt = max(len(kept)-1, 0)
	}
	for _, id := range consoleIDs {
		delete(a.runsByConsole, id)
	}
}

func (a *root) runSQL() {
	a.pollExternalFiles(true)
	if a.externalBlocked() {
		return
	}
	text := a.code.Text()
	ranges := a.execRanges(text)
	if len(ranges) == 0 {
		return
	}
	// 有激活控制台时：先落盘，并确保连着它的数据源；没有控制台时沿用临时缓冲执行（不落盘）。
	app.LogInfo("exec", "start", "conn", a.activeConnName(), "console", a.activeConsole, "sql", text)
	if a.activeConsole != "" {
		if err := a.flushActive(); err != nil {
			a.externalFlushFailed(err)
			return
		}
		if err := a.ensureConsoleConn(); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	a.resetStmtRuns(ranges)
	a.startStmt(a.activeStmt) // 跳过左侧已钉住的结果
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

// textSlice 安全截取 [start,end)：偏移可能来自已被编辑的正文，越界时返回空串。
func textSlice(text string, start, end int) string {
	if start < 0 || start >= end || end > len(text) {
		return ""
	}
	return text[start:end]
}

// resetStmtRuns 为本次执行准备运行时：单条语句沿用当前控制台的运行态（兼容既有行为），
// 多条语句各建一个（id = consoleID#i）。
func (a *root) resetStmtRuns(ranges []app.SQLStatement) {
	// 保留已钉住的结果在最前（左），本次新结果追加到右侧（对照 db-k）。
	pinned := make([]stmtRun, 0, len(a.stmtRuns))
	for _, run := range a.stmtRuns {
		if run.pinned {
			pinned = append(pinned, run)
			continue
		}
		// 丢弃的非 pinned 旧结果：连同 pin 专用运行态与分页缓存一起清理。
		if strings.Contains(run.runtimeID, "#pin") {
			delete(a.consoles, run.runtimeID)
			delete(a.fetchStates, run.runtimeID)
		}
	}
	multi := len(ranges) > 1
	text := a.code.Text()
	runs := make([]stmtRun, 0, len(pinned)+len(ranges))
	runs = append(runs, pinned...)
	for i, st := range ranges {
		id := a.activeConsole
		if multi {
			id = fmt.Sprintf("%s#%d", a.activeConsole, i)
		}
		a.nextTabNo++
		runs = append(runs, stmtRun{stmt: st, sql: textSlice(text, st.Start, st.End), runtimeID: id, tabNo: a.nextTabNo})
	}
	a.stmtRuns = runs
	a.activeStmt = len(pinned)
	a.refreshStmtBackgrounds()
}

// startStmt 执行第 i 条语句；完成回调里顺序推进下一条（失败即停）。
func (a *root) startStmt(i int) {
	if i < 0 || i >= len(a.stmtRuns) {
		return
	}
	a.stmtRuns[i].started = true
	a.activeStmt = i
	sql := a.stmtRuns[i].sql
	a.lastExecSQL = sql
	rt := a.runtimeFor(a.stmtRuns[i].runtimeID)
	rt.SetOnDone(a.stmtDone(i))
	sess := a.activeSession()
	if sess == nil {
		app.LogWarn("exec", "no session", "index", i)
		return
	}
	if !rt.RunTimeout(sess, sql, a.queryTimeoutFor()) {
		app.LogWarn("exec", "statement not started", "index", i)
	}
	a.refreshStmtBackgrounds()
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

// stmtDone 返回第 i 条语句的完成回调：记日志，并在成功时推进下一条。
func (a *root) stmtDone(i int) func(*engine.QueryResult, string, time.Duration) {
	// 捕获运行态 id：回调里不能再按 i 索引 stmtRuns（切控制台或 pin 重排后索引会变）。
	rtID := a.stmtRuns[i].runtimeID
	logDone := a.execDoneLogger(rtID)
	// 在主线程捕获历史所需字段（回调在后台协程）。
	sql := a.stmtRuns[i].sql
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
		rec := func() { a.recordFetchState(rtID, res) }
		if a.win != nil {
			win := a.win
			win.Update(rec)
		} else {
			rec()
		}
		ok := errMsg == ""
		advance := func() {
			a.refreshStmtBackgrounds()
			if !ok {
				return
			}
			// 找回该运行态当前所在位置，执行它的下一条（找不到说明结果已被清走）。
			for j, run := range a.stmtRuns {
				if run.runtimeID == rtID {
					a.startStmt(j + 1)
					return
				}
			}
		}
		if a.win != nil {
			win := a.win
			win.Update(advance)
			return
		}
		advance()
	}
}

// ensureConsoleConn 保证已连接的控制台正是执行目标；否则连上它的数据源。
func (a *root) ensureConsoleConn() error {
	m := a.consoleMgr()
	if m == nil {
		return errors.New("控制台不可用")
	}
	rec, ok := m.Active()
	if !ok {
		return errors.New("没有激活的控制台")
	}
	if a.connected(rec.ConnectionID) {
		a.touchSession(rec.ConnectionID)
		return nil
	}
	p, ok := a.store.Get(rec.ConnectionID)
	if !ok {
		return errors.New("控制台的数据源已删除")
	}
	return a.openConn(p)
}
