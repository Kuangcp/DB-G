package ui

import "testing"

func TestSyncStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.gitSync.Prefs.Branch = "main"
	a.gitSync.Busy = true
	a.gitSync.Findings = nil
	if a.gitSync.Prefs.Branch != "main" || !a.gitSync.Busy {
		t.Fatalf("sync state = %+v", a.gitSync)
	}
}

func TestInstallStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.install.Busy = true
	a.install.Err = "boom"
	if !a.install.Busy || a.install.Err != "boom" {
		t.Fatalf("install state = %+v", a.install)
	}
}

func TestCellViewStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.cellView.Wrap = true
	a.cellView.Title = "value"
	a.cellView.ModalW = 640
	if !a.cellView.Wrap || a.cellView.Title != "value" || a.cellView.ModalW != 640 {
		t.Fatalf("cell view state = %+v", a.cellView)
	}
}

func TestTreeStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.tree.ObjSearch = "users"
	a.tree.ConnSchema = map[string]string{"c1": "public"}
	a.tree.Selected = "c1"
	if a.tree.ObjSearch != "users" || a.tree.ConnSchema["c1"] != "public" || a.tree.Selected != "c1" {
		t.Fatalf("tree state = %+v", a.tree)
	}
}
