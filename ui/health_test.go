package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func TestConnHealthAndProfileTransfer(t *testing.T) {
	a := newConsoleRoot(t)
	good := app.ConnProfile{Name: "good", Kind: "sqlite", Path: seedConnFile(t)}
	badPath := filepath.Join(t.TempDir(), "gone.db")
	f, err := os.Create(badPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	bad := app.ConnProfile{Name: "bad", Kind: "sqlite", Path: badPath}
	if err := a.store.Add(good); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Add(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(badPath); err != nil { // 加完再删，模拟文件丢失
		t.Fatal(err)
	}
	a.checkConnHealthSync()
	var goodID, badID string
	for _, p := range a.store.List() {
		if p.Name == "good" {
			goodID = p.ID
		} else {
			badID = p.ID
		}
	}
	if a.connStatusOf(goodID) != "ok" {
		t.Fatalf("good status: %q", a.connStatusOf(goodID))
	}
	if len(a.connStatusOf(badID)) < 4 || a.connStatusOf(badID)[:4] != "fail" {
		t.Fatalf("bad status: %q", a.connStatusOf(badID))
	}

	// 导出 / 导入（导入到空库）
	out := filepath.Join(t.TempDir(), "conns.json")
	n, err := a.exportProfiles(out)
	if err != nil || n != 2 {
		t.Fatalf("export: n=%d err=%v", n, err)
	}
	b := newConsoleRoot(t)
	if _, err := b.importProfiles(out); err != nil {
		t.Fatal(err)
	}
	// 「bad」的 sqlite 文件已被删除，导入时校验不通过会被跳过（记日志）——这是预期。
	if got := len(b.store.List()); got != 1 {
		t.Fatalf("imported conns: %d", got)
	}
	// 再导一次：去重后新增 0
	if n, err := b.importProfiles(out); err != nil || n != 0 {
		t.Fatalf("re-import: n=%d err=%v", n, err)
	}
}

func TestSettingsHasProfileTransferButtons(t *testing.T) {
	a := newConsoleRoot(t)
	tt := ui.NewTester(a.settingsView, 560, 620)
	tt.Frame()
	for _, want := range []string{"连接档案", "导出连接（不含密码）", "导入连接…"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %q", want, tt.Texts())
		}
	}
}
