package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// 打开数据源 B 失败时，错误提示不能渲染在当前控制台（数据源 A）的顶栏里：
// 改成底部浮窗、自动消失，且不影响 A 的会话。
func TestConnFailureShowsToastNotTopBar(t *testing.T) {
	a := newConsoleRoot(t)
	profA := seedConsoleConn(t, a, "甲")
	badPath := seedConnFile(t)
	if err := a.store.Add(app.ConnProfile{Name: "乙", Kind: "sqlite", Path: badPath}); err != nil {
		t.Fatal(err)
	}
	bad := a.store.List()[1]
	if err := os.Remove(badPath); err != nil {
		t.Fatal(err)
	}
	if err := a.openConn(profA); err != nil {
		t.Fatal(err)
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.mgr.Create(profA.ID, "q")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate console")
	}

	a.expandConnAndTree(bad)
	if a.connErrOf(bad.ID) == "" {
		t.Fatal("失败原因要记在数据源名下")
	}
	if !a.connected(profA.ID) {
		t.Fatal("B 打开失败不该影响 A 的会话")
	}

	tt := ui.NewTester(rootView(a), 1200, 800)
	tt.Frame()
	// 浮窗文本带原始驱动错误，按前缀从实际文案里取完整那一行。
	full := ""
	for _, msg := range tt.Texts() {
		if strings.Contains(msg, "数据源 乙 连接失败") {
			full = msg
			break
		}
	}
	if full == "" {
		t.Fatalf("应弹出错误浮窗: %q", tt.Texts())
	}
	// 浮窗在窗口底部：顶栏（控制台 A）里不应出现这条错误。
	rect, ok := tt.Find(full)
	if !ok {
		t.Fatalf("找不到浮窗文本 %q: %q", full, tt.Texts())
	}
	if rect.Y < 400 {
		t.Fatalf("错误提示跑到了顶栏区域: y=%v", rect.Y)
	}

	tt.Advance(toastTimeout + time.Second)
	if tt.HasText("数据源 乙 连接失败") {
		t.Fatalf("浮窗应在超时后自动消失: %q", tt.Texts())
	}
}

// 浮窗右侧的关闭按钮能立即关掉提示（不必等超时）。
func TestToastCloseButtonDismisses(t *testing.T) {
	a := newConsoleRoot(t)
	a.toastError("可以手动关掉")
	tt := ui.NewTester(rootView(a), 800, 600)
	tt.Frame()
	if !tt.HasText("可以手动关掉") {
		t.Fatalf("浮窗没显示: %q", tt.Texts())
	}
	if err := tt.Click(locales.CommonClose()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("可以手动关掉") {
		t.Fatalf("点关闭后提示应消失: %q", tt.Texts())
	}
}
