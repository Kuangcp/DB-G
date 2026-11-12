package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	updaterNative "github.com/egoist/mygo/plugins/updater/native"
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

// stringList 是可重复的字符串参数（-o a.sql -o b.sql）。
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// secondLaunch 是一个第二实例转发来的命令行。
type secondLaunch struct {
	args []string
	dir  string
}

// openPathsFromArgs 解析第二实例转发来的参数，并把相对路径改到它启动时的工作目录。
func openPathsFromArgs(args []string, workingDir string) []string {
	fs := flag.NewFlagSet("db-g", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var files stringList
	fs.Var(&files, "o", "")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	for i, p := range files {
		if !filepath.IsAbs(p) {
			files[i] = filepath.Join(workingDir, p)
		}
	}
	return files
}

func main() {
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	var openFiles stringList
	flag.Var(&openFiles, "o", "打开 SQL 文件（等价于拖入窗口，可重复）")
	flag.Parse()
	if *showVersion {
		fmt.Println(app.DisplayName, app.Version())
		return
	}
	// 开发构建用独立的应用名，避免和正式安装版共用单实例锁/IPC。
	if !app.ReleaseBuild() {
		mygo.App.SetName("db-g.dev")
	}
	// 已有实例时把本次命令行交给它，由它打开 SQL；本进程直接退出。
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// 先把第二实例的参数收下来；root 建好前到达的请求排队，之后再处理。
	var (
		secondHandler func([]string, string)
		secondPending []secondLaunch
	)
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		if secondHandler != nil {
			secondHandler(args, workingDir)
			return
		}
		secondPending = append(secondPending, secondLaunch{args: args, dir: workingDir})
	})
	if err := app.InitLogs(app.DefaultLogDir()); err != nil {
		log.Printf("日志初始化失败: %v", err)
	}
	defer app.CloseLogs()
	// 原生更新窗口，不依赖 WebView；开发构建会自动报告更新不可用。
	mygo.Use(updaterNative.Plugin)
	// 主窗口的位置/大小记在配置目录；和连接库一样区分发布/开发构建。
	mygo.App.SetWindowStateStore(windowStateStore{path: filepath.Join(app.AppConfigDir(), "window-state.json")})
	app.LogInfo("startup", "db-g start", "config", app.DefaultStorePath(), "log_dir", app.DefaultLogDir())

	r := ui.NewRoot()
	secondHandler = func(args []string, workingDir string) {
		r.FocusWindow()
		if paths := openPathsFromArgs(args, workingDir); len(paths) > 0 {
			r.OpenSQLFiles(paths)
		}
	}
	for _, msg := range secondPending {
		secondHandler(msg.args, msg.dir)
	}
	secondPending = nil
	r.SetAppIconPNG(appIconPNG)
	themePrefs, _ := app.LoadThemePrefs(app.DefaultThemePrefsPath())
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           app.DisplayName,
			Width:           1280,
			Height:          800,
			StateKey:        "main",
			Content:         mygoUI.View(r.View),
			Frameless:       true,
			CornerRadius:    themePrefs.Radius + 4,
			BackgroundColor: ui.WindowBackgroundColor(themePrefs),
		})
		if err := win.SetIcon(appIconPNG); err != nil {
			app.LogWarn("startup", "set window icon failed", "err", err.Error())
		}
		if mygo.App.Dock != nil {
			_ = mygo.App.Dock.SetIcon(appIconPNG)
		}
		r.Attach(win)
		if len(openFiles) > 0 {
			r.OpenSQLFiles(openFiles)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	r.FlushAll()
}
