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
	a.tabs.LastExecSQL = "select * from user"
	a.setActiveText("select * from user")
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT * FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"

	a.fixErrorSQL()
	if !a.ai.Fix.open || a.ai.Fix.loading || a.ai.Fix.err != "" {
		t.Fatalf("fix state: %+v", a.ai.Fix)
	}
	if a.ai.Fix.fixed != "SELECT * FROM users;" || a.ai.Fix.diff == nil {
		t.Fatalf("fixed: %q diff=%v", a.ai.Fix.fixed, a.ai.Fix.diff)
	}
	a.applyAIFix(false)
	if got := a.code.Text(); !strings.Contains(got, "SELECT * FROM users;") {
		t.Fatalf("editor: %q", got)
	}
	if a.ai.Fix.open {
		t.Fatal("modal should close after apply")
	}
}

func TestAIFixRejectsMissingCodeBlock(t *testing.T) {
	a := newConsoleRoot(t)
	a.con.SetOutcome(nil, "boom")
	a.tabs.LastExecSQL = "select 1"
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "抱歉，我无法修复"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.fixErrorSQL()
	if a.ai.Fix.err == "" || a.ai.Fix.fixed != "" {
		t.Fatalf("state: %+v", a.ai.Fix)
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
	a.tabs.LastExecSQL = "select * from user"
	a.setActiveText("select * from user")
	a.ai.TestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "```sql\nSELECT * FROM users;\n```"},
		{Kind: ai.EventDone},
	}}
	a.ai.Prefs.Model = "fake"
	a.fixErrorSQL()
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("AI 修复 SQL") || !tt.HasText("应用修复") {
		t.Fatalf("fix modal missing: %q", tt.Texts())
	}
}
