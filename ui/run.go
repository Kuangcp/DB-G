package ui

import (
	"context"
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
	a.con.SwapSession(sess)
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
		a.con.SwapSession(nil)
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
	a.con.Run(a.ed.Buf.Slice(a.selStart, a.selEnd))
}
