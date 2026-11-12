package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/engine"
)

type fakePlanSession struct {
	opts engine.ExplainOptions
	sql  string
	err  error
	caps engine.Capabilities
}

func (s *fakePlanSession) Open(context.Context) error { return nil }
func (s *fakePlanSession) Close() error               { return nil }
func (s *fakePlanSession) Cancel() bool               { return false }
func (s *fakePlanSession) Caps() engine.Capabilities {
	if s.caps != 0 {
		return s.caps
	}
	return engine.CapExplain | engine.CapExplainAnalyze | engine.CapObjects | engine.CapColumns
}
func (s *fakePlanSession) RunStatement(context.Context, string) (*engine.QueryResult, error) {
	return &engine.QueryResult{}, nil
}
func (s *fakePlanSession) Explain(_ context.Context, sql string, opts engine.ExplainOptions) (*engine.QueryResult, error) {
	s.sql, s.opts = sql, opts
	if s.err != nil {
		return nil, s.err
	}
	return &engine.QueryResult{
		Columns: []string{"id", "parent", "notused", "detail"},
		Rows: [][]any{
			{int64(2), int64(0), int64(0), "SCAN users"},
		},
	}, nil
}

func planRoot(t *testing.T) (*root, *fakePlanSession) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	rec, err := a.consoleMgr().Create(prof.ID, "计划")
	if err != nil {
		t.Fatal(err)
	}
	if !a.activateConsole(rec.ID) {
		t.Fatal("activate")
	}
	sess := &fakePlanSession{}
	a.testSetSession(prof.ID, sess)
	a.setActiveText("select * from users")
	return a, sess
}

func TestRunExplainParsesPlan(t *testing.T) {
	a, sess := planRoot(t)
	a.runExplain(false)
	if sess.sql != "select * from users" {
		t.Fatalf("explain sql: %q", sess.sql)
	}
	if !a.plan.open || a.plan.loading || len(a.plan.nodes) != 1 {
		t.Fatalf("plan: %+v", a.plan)
	}
	if a.plan.nodes[0].Op != "SCAN users" {
		t.Fatalf("nodes: %+v", a.plan.nodes)
	}
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if !tt.HasText("执行计划") || !tt.HasText("SCAN users") {
		t.Fatalf("plan pane missing: %q", tt.Texts())
	}
}

func TestRunExplainAnalyzeOnlySelect(t *testing.T) {
	a, sess := planRoot(t)
	a.setActiveText("update users set name = 'x'")
	a.runExplain(true)
	if a.plan.open || !strings.Contains(a.consoleErr, "只允许 SELECT") {
		t.Fatalf("non-select analyze: open=%v err=%q", a.plan.open, a.consoleErr)
	}
	a.setActiveText("select * from users")
	a.runExplain(true)
	if !sess.opts.Analyze {
		t.Fatalf("analyze opt: %+v", sess.opts)
	}
}

func TestDiagnosePlanSendsExecutionContext(t *testing.T) {
	a, _ := planRoot(t)
	a.runExplain(false)
	a.aiTestProvider = &fakeAIProvider{events: []ai.Event{
		{Kind: ai.EventText, Text: "建议加索引"},
		{Kind: ai.EventDone},
	}}
	a.aiPrefs.Model = "fake"
	a.diagnosePlan()
	if !a.aiChat.open || len(a.aiChat.messages) != 2 {
		t.Fatalf("diagnose chat: open=%v messages=%+v", a.aiChat.open, a.aiChat.messages)
	}
	p := a.aiTestProvider.(*fakeAIProvider)
	if !strings.Contains(p.req.Messages[1].Content, "执行计划") ||
		!strings.Contains(p.req.Messages[1].Content, "SCAN users") {
		t.Fatalf("plan missing from prompt:\n%s", p.req.Messages[1].Content)
	}
}

func TestRunExplainAnalyzeUnsupported(t *testing.T) {
	a, sess := planRoot(t)
	sess.caps = engine.CapExplain | engine.CapObjects | engine.CapColumns
	a.setActiveText("select * from users")
	a.runExplain(true)
	if a.plan.open || !strings.Contains(a.consoleErr, "不支持 EXPLAIN ANALYZE") {
		t.Fatalf("unsupported analyze: open=%v err=%q", a.plan.open, a.consoleErr)
	}
	a.runExplain(false)
	tt := ui.NewTester(rootView(a), 1400, 800)
	tt.Frame()
	if tt.HasText("ANALYZE 重跑") {
		t.Fatalf("unsupported driver must hide analyze button: %q", tt.Texts())
	}
}
