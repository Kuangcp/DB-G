package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"db-g/app"
	"db-g/engine"
)

func (a *root) openSQLite(path string) error {
	return a.openConn(app.ConnProfile{Name: filepath.Base(path), Kind: "sqlite", Path: path})
}

// engineProfile 把连接档案翻成引擎的 Profile：档案多出来的协议和连接参数在这里
// 一并带上，三处打开连接（正常打开/健康检查/测试连接）都用它。
func engineProfile(p app.ConnProfile) engine.Profile {
	return engine.Profile{
		Kind: p.Kind, Name: p.Name, DSN: p.Path,
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database,
		Protocol: p.Protocol, Params: p.Params,
	}
}

func (a *root) openConn(p app.ConnProfile) error {
	app.LogInfo("conn", "open", "kind", p.Kind, "name", p.Name, "dsn", app.RedactDSN(p.Path))
	sess, err := engine.Open(engineProfile(p))
	if err != nil {
		a.connErr = err.Error()
		a.failedConnID = p.ID
		a.setTreeErr(p.ID, a.connErr)
		app.LogError("conn", "open failed", "kind", p.Kind, "name", p.Name, "dsn", app.RedactDSN(p.Path), "err", err.Error())
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.connErr = err.Error()
		a.failedConnID = p.ID
		a.setTreeErr(p.ID, a.connErr)
		return err
	}
	a.setSession(p.ID, sess)
	if a.catalog != nil {
		a.catalog.invalidate(p.ID) // 重连后目录可能变了
	}
	a.connName = p.Name
	a.activeConnID = p.ID
	a.failedConnID = ""
	a.connErr = ""
	if a.store != nil && p.ID != "" {
		a.store.SetActive(p.ID)
	}
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
	if a.failedConnID == id {
		a.failedConnID = ""
		a.connErr = ""
	}
	if a.activeConnID == id {
		a.setSession("", nil)
		a.connName = ""
		a.activeConnID = ""
		a.connErr = ""
		if act, ok := a.store.Active(); ok && act.ID == id {
			a.store.SetActive("")
		}
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
}

// stmtRun 是一次执行里的一条语句：范围、运行时 id、是否已开始。
type stmtRun struct {
	stmt      app.SQLStatement
	runtimeID string
	started   bool
}

func (a *root) runSQL() {
	text := a.code.Text()
	ranges := a.execRanges(text)
	if len(ranges) == 0 {
		return
	}
	// 有激活控制台时：先落盘，并确保连着它的数据源；没有控制台时沿用临时缓冲执行（不落盘）。
	app.LogInfo("exec", "start", "conn", a.connName, "console", a.activeConsole, "sql", text)
	if a.activeConsole != "" {
		if err := a.flushActive(); err != nil {
			return
		}
		if err := a.ensureConsoleConn(); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	a.resetStmtRuns(ranges)
	a.startStmt(0)
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

// resetStmtRuns 为本次执行准备运行时：单条语句沿用当前控制台的运行态（兼容既有行为），
// 多条语句各建一个（id = consoleID#i）。
func (a *root) resetStmtRuns(ranges []app.SQLStatement) {
	a.stmtRuns = nil
	a.activeStmt = 0
	multi := len(ranges) > 1
	for i, st := range ranges {
		id := a.activeConsole
		if multi {
			id = fmt.Sprintf("%s#%d", a.activeConsole, i)
		}
		a.stmtRuns = append(a.stmtRuns, stmtRun{stmt: st, runtimeID: id})
	}
	a.refreshStmtBackgrounds()
}

// startStmt 执行第 i 条语句；完成回调里顺序推进下一条（失败即停）。
func (a *root) startStmt(i int) {
	if i < 0 || i >= len(a.stmtRuns) {
		return
	}
	a.stmtRuns[i].started = true
	a.activeStmt = i
	stmt := a.stmtRuns[i].stmt
	sql := a.code.Text()[stmt.Start:stmt.End]
	a.lastExecSQL = sql
	rt := a.runtimeFor(a.stmtRuns[i].runtimeID)
	rt.SetOnDone(a.stmtDone(i))
	if !rt.Run(a.sess, sql) {
		app.LogWarn("exec", "statement not started", "index", i)
	}
	a.refreshStmtBackgrounds()
}

// stmtDone 返回第 i 条语句的完成回调：记日志，并在成功时推进下一条。
func (a *root) stmtDone(i int) func(*engine.QueryResult, string, time.Duration) {
	logDone := a.execDoneLogger(a.stmtRuns[i].runtimeID)
	// 在主线程捕获历史所需字段（回调在后台协程）。
	stmt := a.stmtRuns[i].stmt
	sql := a.code.Text()[stmt.Start:stmt.End]
	connID, connName := a.sessConnID, a.connName
	return func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		logDone(res, errMsg, dur)
		if a.history != nil {
			e := app.HistoryEntry{ConnID: connID, ConnName: connName, SQL: sql, DurMS: dur.Milliseconds(), OK: errMsg == "", Err: errMsg}
			if res != nil {
				e.Rows = len(res.Rows)
			}
			_ = a.history.Add(e)
		}
		rec := func() { a.recordFetchState(a.stmtRuns[i].runtimeID, res) }
		if a.win != nil {
			win := a.win
			win.Update(rec)
		} else {
			rec()
		}
		ok := errMsg == ""
		advance := func() {
			a.refreshStmtBackgrounds()
			if ok {
				a.startStmt(i + 1)
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
	if a.sess != nil && a.sessConnID == rec.ConnectionID {
		return nil
	}
	p, ok := a.store.Get(rec.ConnectionID)
	if !ok {
		return errors.New("控制台的数据源已删除")
	}
	return a.openConn(p)
}
