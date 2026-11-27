package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// 设置窗口/偏好类的错误不能写主窗口顶栏：留在设置内联提示并弹 toast。
func TestSettingsErrorsDoNotUseTopBar(t *testing.T) {
	a := newConsoleRoot(t)
	a.consoleErr = "keep"

	a.reportSettingsResult("profiles", "导入失败：bad file", true)

	if a.consoleErr != "keep" {
		t.Fatalf("consoleErr = %q, want unchanged", a.consoleErr)
	}
	if len(a.toastQueue) != 1 || a.toastQueue[0].Title != "导入失败：bad file" {
		t.Fatalf("toast = %+v", a.toastQueue)
	}
	notice, ok := a.settingsNotice["profiles"]
	if !ok || notice.text != "导入失败：bad file" || !notice.err {
		t.Fatalf("settings notice = %+v %v", notice, ok)
	}
}

func TestSettingsSuccessDoesNotToast(t *testing.T) {
	a := newConsoleRoot(t)
	a.consoleErr = "keep"
	a.reportSettingsResult("profiles", "已导出 2 个连接", false)
	if a.consoleErr != "keep" {
		t.Fatalf("consoleErr = %q", a.consoleErr)
	}
	if len(a.toastQueue) != 0 {
		t.Fatalf("success must not toast: %+v", a.toastQueue)
	}
	if n := a.settingsNotice["profiles"]; n.err || n.text == "" {
		t.Fatalf("notice = %+v", n)
	}
}

// 快捷键存盘失败属于设置面板：写入内联提示，不占顶栏。
func TestKeymapSaveFailureStaysInSettings(t *testing.T) {
	a := newConsoleRoot(t)
	block := filepath.Join(t.TempDir(), "block")
	if err := os.WriteFile(block, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.keymapPath = filepath.Join(block, "keymap.json")
	a.consoleErr = "keep"

	a.saveKeymap()

	if a.consoleErr != "keep" {
		t.Fatalf("consoleErr = %q, want unchanged", a.consoleErr)
	}
	if got := a.settingsNotice["keymap"]; !got.err || got.text == "" {
		t.Fatalf("keymap notice = %+v", got)
	}
}
