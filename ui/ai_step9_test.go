package ui

import (
	"strings"
	"testing"
	"time"

	"db-g/ai"
	"db-g/app"
	"db-g/engine"
)

func TestAIPreviewOneOffContextToggles(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "toggles")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select id from users")
	a.catalog.finish(prof.ID, []string{"users"}, nil, "")
	a.catalog.finishColumns(prof.ID, "users", []engine.Column{{Name: "id", Type: "INTEGER"}}, "")
	a.aiPrefs.AllowDDL = true
	a.aiPrefs.AllowSampleRows = true
	a.aiPrefs.AllowHistory = true
	a.aiPrefs.AllowErrorLogs = true
	a.aiPrefs.Model = "fake"
	a.aiChat.input = "看一下"
	a.history = app.OpenHistory(t.TempDir())
	if err := a.history.Add(app.HistoryEntry{ConnID: prof.ID, SQL: "select 1"}); err != nil {
		t.Fatal(err)
	}
	a.consoleErr = "boom"
	a.aiDDL = map[string]aiDDLEntry{
		aiDDLKey(prof.ID, "", "users"): {ddl: "CREATE TABLE users (id INTEGER)", at: time.Now()},
	}
	a.runtimeFor(rec.ID).SetOutcome(&engine.QueryResult{
		IsQuery: true, Columns: []string{"id"}, Rows: [][]any{{int64(1)}},
	}, "")

	a.openAIPreview()
	body := a.aiDebug.body
	for _, want := range []string{"Current Result", "Sample", "Recent Queries", "Recent Errors", "DDL:"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q:\n%s", want, body)
		}
	}

	a.aiDebug.previewOpts = aiEgressPolicy{}
	a.refreshAIPreview()
	body = a.aiDebug.body
	for _, gone := range []string{"Sample", "Recent Queries", "Recent Errors", "DDL:"} {
		if strings.Contains(body, gone) {
			t.Fatalf("one-off toggle leaked %q:\n%s", gone, body)
		}
	}
	if !strings.Contains(body, "Current Result") {
		t.Fatalf("result shape must always remain:\n%s", body)
	}
}

func TestAITokenCalibrationFromUsage(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	if err := a.openConn(prof); err != nil {
		t.Fatal(err)
	}
	rec, err := a.consoleMgr().Create(prof.ID, "cal")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	a.setActiveText("select 1")
	a.aiPrefs.Model = "cal-model"
	p := &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventUsage, Usage: &ai.Usage{PromptTokens: 100000, CompletionTokens: 10, TotalTokens: 100010}},
		{Kind: ai.EventDone},
	}}
	a.aiTestProvider = p
	a.aiChat.input = "hi"
	a.sendAI()
	if got := a.aiTokenScale["cal-model"]; got <= 1 {
		t.Fatalf("scale not calibrated: %v", got)
	}
	plain := ai.Budget{ContextWindow: 8192, MaxOutput: 1000}
	scaled := a.aiBudget("chat", 1000)
	if scaled.EstimateScale <= 1 || scaled.Limit() >= plain.Limit() {
		t.Fatalf("calibrated budget: %+v limit=%d", scaled, scaled.Limit())
	}
}
