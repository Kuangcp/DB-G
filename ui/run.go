package ui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"db-g/engine"
)

func (a *root) openSQLite(path string) error {
	sess, err := engine.Open(engine.Profile{Kind: "sqlite", Name: filepath.Base(path), DSN: path})
	if err != nil {
		a.openErr = err.Error()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.openErr = err.Error()
		return err
	}
	a.con.SwapSession(sess)
	a.dbName = filepath.Base(path)
	a.openErr = ""
	return nil
}

func (a *root) openDatabase() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "打开 SQLite 数据库",
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil {
			win.Update(func() { a.openErr = err.Error() })
			return
		}
		if len(paths) == 0 {
			return
		}
		path := paths[0]
		win.Update(func() { a.openSQLite(path) })
	}()
}

func (a *root) runSQL() {
	a.con.Run(a.ed.Buf.String())
}
