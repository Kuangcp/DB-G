package ui

import (
	"context"
	"path/filepath"
	"time"

	"db-g/engine"
)

func (a *root) openSQLite(path string) error {
	sess, err := engine.Open(engine.Profile{Kind: "sqlite", Name: filepath.Base(path), DSN: path})
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
	a.connName = filepath.Base(path)
	a.connErr = ""
	return nil
}

func (a *root) runSQL() {
	a.con.Run(a.ed.Buf.String())
}
