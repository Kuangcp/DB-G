package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"

	"github.com/egoist/mygo"
	mygoUI "github.com/egoist/mygo/ui"

	"db-g/app"
	_ "db-g/driver/clickhouse"
	_ "db-g/driver/mysql"
	_ "db-g/driver/postgres"
	_ "db-g/driver/sqlite"
	"db-g/ui"
)

//go:embed assets/icon-256.png
var appIconPNG []byte

func main() {
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println("db-g", app.Version())
		return
	}
	if err := app.InitLogs(app.DefaultLogDir()); err != nil {
		log.Printf("日志初始化失败: %v", err)
	}
	defer app.CloseLogs()
	app.LogInfo("startup", "db-g start", "config", app.DefaultStorePath(), "log_dir", app.DefaultLogDir())

	r := ui.NewRoot()
	r.SetAppIconPNG(appIconPNG)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   "db-g",
			Width:   1280,
			Height:  800,
			Content: mygoUI.View(r.View),
		})
		if err := win.SetIcon(appIconPNG); err != nil {
			app.LogWarn("startup", "set window icon failed", "err", err.Error())
		}
		if mygo.App.Dock != nil {
			_ = mygo.App.Dock.SetIcon(appIconPNG)
		}
		r.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	r.FlushAll()
}
