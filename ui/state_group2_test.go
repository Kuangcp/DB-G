package ui

import (
	"testing"

	"db-g/engine"
)

func TestSettingsStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.settings.Section = 2
	a.settings.CaptureAction = "exec"
	a.settings.FontPrefs.Editor.Size = 14
	a.settings.Notice = map[string]settingsNotice{"theme": {text: "warn", err: true}}
	if a.settings.Section != 2 || a.settings.CaptureAction != "exec" || a.settings.FontPrefs.Editor.Size != 14 {
		t.Fatalf("section=%d capture=%q font=%v", a.settings.Section, a.settings.CaptureAction, a.settings.FontPrefs.Editor.Size)
	}
	if n := a.settings.Notice["theme"]; !n.err || n.text != "warn" {
		t.Fatalf("notice = %+v", n)
	}
}

func TestConsoleTabsStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.tabs.ActiveConsole = "c1"
	a.tabs.NextTabNo = 3
	a.tabs.PageStates = map[string]*pageState{"c1": {Page: 2}}
	if a.tabs.ActiveConsole != "c1" || a.tabs.NextTabNo != 3 || a.tabs.PageStates["c1"].Page != 2 {
		t.Fatalf("tabs state = %+v", a.tabs)
	}
}

func TestAIStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.ai.Prefs.Model = "gpt-test"
	a.ai.Chat.open = true
	a.ai.Plan.open = true
	if a.ai.Prefs.Model != "gpt-test" || !a.ai.Chat.open || !a.ai.Plan.open {
		t.Fatalf("model=%q chat=%v plan=%v", a.ai.Prefs.Model, a.ai.Chat.open, a.ai.Plan.open)
	}
}

func TestEditStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.edit.Busy = true
	a.edit.CellOpen = true
	a.edit.Status = "saved"
	a.edit.PreviewLines = []string{"UPDATE t SET x=1"}
	if !a.edit.Busy || !a.edit.CellOpen || a.edit.Status != "saved" || len(a.edit.PreviewLines) != 1 {
		t.Fatalf("edit state fields not grouped")
	}
}

func TestSessionStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.session.Opening = map[string]bool{"c1": true}
	a.session.ServerInfo = map[string]engine.ServerInfo{"c1": {Database: "db"}}
	a.session.Use = map[string]uint64{"c1": 2}
	if !a.session.Opening["c1"] || a.session.ServerInfo["c1"].Database != "db" || a.session.Use["c1"] != 2 {
		t.Fatal("session state fields not grouped")
	}
}

func TestModalStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.modal.Kind = "rename"
	a.modal.DDLOpen = true
	a.modal.GuardOpen = true
	a.modal.PaletteJumpSel = [2]int{1, 3}
	if a.modal.Kind != "rename" || !a.modal.DDLOpen || !a.modal.GuardOpen || a.modal.PaletteJumpSel != [2]int{1, 3} {
		t.Fatal("modal state fields not grouped")
	}
}

func TestConnFormStateIncludesDeleteAndTest(t *testing.T) {
	a := newRoot("")
	a.connForm.DeleteOpen = true
	a.connForm.Deleting = "c1"
	a.connForm.TestBusy = true
	a.connForm.TestSeq = 4
	if !a.connForm.DeleteOpen || a.connForm.Deleting != "c1" || !a.connForm.TestBusy || a.connForm.TestSeq != 4 {
		t.Fatal("conn form delete/test fields not grouped")
	}
}

func TestFilterStateLivesOnGrid(t *testing.T) {
	a := newRoot("")
	a.grid.filterOpen = true
	a.grid.filterCol = 2
	a.grid.filterText = "x"
	if !a.grid.filterOpen || a.grid.filterCol != 2 || a.grid.filterText != "x" {
		t.Fatal("filter state not on grid")
	}
}

func TestExternalAndToastStateIsGrouped(t *testing.T) {
	a := newRoot("")
	a.external.Msg = "saved"
	a.external.PickOpen = true
	a.toasts.Seq = 3
	if a.external.Msg != "saved" || !a.external.PickOpen || a.toasts.Seq != 3 {
		t.Fatal("external/toast state not grouped")
	}
}
