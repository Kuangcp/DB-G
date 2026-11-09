package ui

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
)

func TestAIFixAppliesSuggestedSQL(t *testing.T) {
	a := newConsoleRoot(t)
	a.con.SetOutcome(nil, `column "user" does not exist`)
	a.lastExecSQL = "select * from user"
	a.setActiveText("select * from user")
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT * FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"

	a.fixErrorSQL()
	if !a.aiFix.open || a.aiFix.loading || a.aiFix.err != "" {
		t.Fatalf("fix state: %+v", a.aiFix)
	}
	if a.aiFix.fixed != "SELECT * FROM users;" || a.aiFix.diff == nil {
		t.Fatalf("fixed: %q diff=%v", a.aiFix.fixed, a.aiFix.diff)
	}
	a.applyAIFix(false)
	if got := a.code.Text(); !strings.Contains(got, "SELECT * FROM users;") {
		t.Fatalf("editor: %q", got)
	}
	if a.aiFix.open {
		t.Fatal("modal should close after apply")
	}
}

func TestAIFixRejectsMissingCodeBlock(t *testing.T) {
	a := newConsoleRoot(t)
	a.con.SetOutcome(nil, "boom")
	a.lastExecSQL = "select 1"
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "抱歉，我无法修复"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.fixErrorSQL()
	if a.aiFix.err == "" || a.aiFix.fixed != "" {
		t.Fatalf("state: %+v", a.aiFix)
	}
}

func TestReplaceStatementRefusesStaleText(t *testing.T) {
	a := newConsoleRoot(t)
	a.setActiveText("select 2")
	if a.replaceStatement("select 1", "select 3") {
		t.Fatal("stale original must not be replaced")
	}
	if got := a.code.Text(); got != "select 2" {
		t.Fatalf("editor changed: %q", got)
	}
}

func TestAIFixModalRenders(t *testing.T) {
	a := newConsoleRoot(t)
	a.con.SetOutcome(nil, "boom")
	a.lastExecSQL = "select * from user"
	a.setActiveText("select * from user")
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT * FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.fixErrorSQL()
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("AI 修复 SQL") || !tt.HasText("应用修复") {
		t.Fatalf("fix modal missing: %q", tt.Texts())
	}
}
