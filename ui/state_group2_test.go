package ui

import "testing"

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
