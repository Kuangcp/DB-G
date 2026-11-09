package ui

import (
	"strings"
	"testing"
)

func TestRecoverPanicKeepsAppAlive(t *testing.T) {
	a := newConsoleRoot(t)
	called := false
	a.safeUpdateFunc(func(fn func()) {
		called = true
		fn()
	}, func() { panic("boom") })
	if !called {
		t.Fatal("updater must run")
	}
	if !strings.Contains(a.consoleErr, "boom") {
		t.Fatalf("panic must be reported in the top bar: %q", a.consoleErr)
	}
	// 后续回调仍然可以执行。
	a.safeUpdateFunc(func(fn func()) { fn() }, func() { a.consoleErr = "ok" })
	if a.consoleErr != "ok" {
		t.Fatalf("app state unusable after panic: %q", a.consoleErr)
	}
}
