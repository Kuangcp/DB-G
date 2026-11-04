package ui

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"db-g/app"
	"db-g/engine"
)

func (a *root) openSQLite(path string) error {
	return a.openConn(app.ConnProfile{Name: filepath.Base(path), Kind: "sqlite", Path: path})
}

func (a *root) openConn(p app.ConnProfile) error {
	sess, err := engine.Open(engine.Profile{
		Kind: p.Kind, Name: p.Name, DSN: p.Path,
		Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database,
	})
	if err != nil {
		a.connErr = err.Error()
		a.failedConnID = p.ID
		a.setTreeErr(p.ID, a.connErr)
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
}

func (a *root) runSQL() {
	if a.selStart == a.selEnd {
		return
	}
	// 有激活控制台时：先落盘，并确保连着它的数据源；没有控制台时沿用临时缓冲执行（不落盘）。
	if a.activeConsole != "" {
		if err := a.flushActive(); err != nil {
			return
		}
		if err := a.ensureConsoleConn(); err != nil {
			a.consoleErr = err.Error()
			return
		}
	}
	a.currentRuntime().Run(a.sess, a.ed.Buf.Slice(a.selStart, a.selEnd))
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
