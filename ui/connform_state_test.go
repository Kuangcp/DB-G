package ui

import "testing"

// 新建/编辑连接的表单状态收进 connForm，不再散落在 root 的 connForm* 字段上。
func TestConnFormStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.openNewConnForm("folder-1")
	if !a.connForm.Open {
		t.Fatal("form must be open")
	}
	if a.connForm.Kind != "sqlite" || a.connForm.Folder != "folder-1" {
		t.Fatalf("kind/folder = %q/%q", a.connForm.Kind, a.connForm.Folder)
	}
	a.connForm.Name = "本地库"
	a.connForm.Path = "/tmp/a.db"
	if p, err := a.connFormProfile(); err != nil || p.Name != "本地库" || p.Path != "/tmp/a.db" {
		t.Fatalf("profile = %+v err=%v", p, err)
	}
}
