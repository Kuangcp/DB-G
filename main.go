package main

import (
	"log"

	"github.com/egoist/mygo"
	mygoUI "github.com/egoist/mygo/ui"

	"db-g/app"
	_ "db-g/driver/mysql"
	_ "db-g/driver/sqlite"
	"db-g/ui"
)

func main() {
	if err := app.InitLogs(app.DefaultLogDir()); err != nil {
		log.Printf("日志初始化失败: %v", err)
	}
	defer app.CloseLogs()
	app.LogInfo("startup", "db-g start", "config", app.DefaultStorePath(), "log_dir", app.DefaultLogDir())

	r := ui.NewRoot()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-g",
			Width:   1280,
			Height:  800,
			Content: mygoUI.View(r.View),
		})
		r.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	r.FlushAll()
}
