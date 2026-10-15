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
	sess, err := engine.Open(engine.Profile{Kind: p.Kind, Name: p.Name, DSN: p.Path})
	if err != nil {
		a.connErr = err.Error()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.connErr = err.Error()
		return err
	}
	a.con.SwapSession(sess)
	a.connName = p.Name
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
	if act, ok := a.store.Active(); ok && act.ID == id {
		a.con.SwapSession(nil)
		a.connName = ""
		a.store.SetActive("")
	}
	_ = a.store.Remove(id)
}

func (a *root) runSQL() {
	a.con.Run(a.ed.Buf.String())
}
