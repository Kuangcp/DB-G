package ai

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func manyTables(n int) []TableContext {
	out := make([]TableContext, 0, n)
	for i := range n {
		cols := make([]ColumnContext, 0, 20)
		for j := range 20 {
			cols = append(cols, ColumnContext{Name: fmt.Sprintf("col_%d_%d", i, j), Type: "text"})
		}
		out = append(out, TableContext{Name: fmt.Sprintf("table_%02d", i), Columns: cols})
	}
	return out
}

func TestBudgetTrimsLowPriorityContext(t *testing.T) {
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql", Database: "shop"},
		ActiveSQL:  "select * from table_00",
		Tables:     manyTables(16),
		History:    []HistoryItem{{SQL: "select 1"}, {SQL: "select 2"}, {SQL: "select 3"}, {SQL: "select 4"}},
		Errors:     []ErrorItem{{SQL: "bad", Err: strings.Repeat("x", 200)}},
		Samples:    []TableSample{{Table: "table_00", Rows: [][]string{{"1"}}}},
	}
	budget := Budget{ContextWindow: 2600, MaxOutput: 900}
	msgs, rep := BuildChat(snapshot, "统计 table_00 的数据", budget)
	if !rep.Trimmed() {
		t.Fatalf("expected trimming, report: %+v", rep)
	}
	if rep.Tokens > rep.Limit {
		t.Fatalf("prompt over budget: %d > %d", rep.Tokens, rep.Limit)
	}
	if rep.Tokens != tokenTotal(msgs) {
		t.Fatalf("report tokens %d != actual %d", rep.Tokens, tokenTotal(msgs))
	}
	if rep.DroppedSamples == 0 || rep.DroppedHistory == 0 {
		t.Fatalf("samples/history should be dropped first: %+v", rep)
	}
	body := msgs[1].Content
	if strings.Contains(body, "# 样本") {
		t.Fatalf("samples must be trimmed:\n%s", body)
	}
	if rep.Tables > len(snapshot.Tables) || rep.Columns > 320 {
		t.Fatalf("report should reflect trimmed schema: %+v", rep)
	}
}

func TestBudgetAlwaysKeepsQuestion(t *testing.T) {
	question := "这个指标怎么算"
	msgs, rep := BuildChat(ContextSnapshot{Connection: ConnInfo{Kind: "sqlite"}}, question,
		Budget{ContextWindow: 2048, MaxOutput: 1200})
	if !strings.Contains(msgs[1].Content, question) {
		t.Fatalf("question must survive the budget:\n%s", msgs[1].Content)
	}
	if rep.Tokens > rep.Limit {
		t.Fatalf("over budget: %d > %d", rep.Tokens, rep.Limit)
	}
}

func TestCompletionTruncationKeepsUTF8(t *testing.T) {
	before := strings.Repeat("-- 中文注释行\n", 4000) + "select * from us"
	after := strings.Repeat("中文后文 ", 4000)
	_, rep := BuildCompletion(ContextSnapshot{
		Connection: ConnInfo{Kind: "sqlite"},
		Tables:     manyTables(16),
	}, before, after, Budget{ContextWindow: 2600, MaxOutput: 300})
	if !rep.Trimmed() {
		t.Fatalf("expected completion trimming: %+v", rep)
	}
	msgs, _ := BuildCompletion(ContextSnapshot{
		Connection: ConnInfo{Kind: "sqlite"},
		Tables:     manyTables(16),
	}, before, after, Budget{ContextWindow: 2600, MaxOutput: 300})
	for _, m := range msgs {
		if !utf8.ValidString(m.Content) {
			t.Fatalf("message is not valid UTF-8: %q", m.Content)
		}
	}
	if rep.Tokens > rep.Limit {
		t.Fatalf("over budget: %d > %d", rep.Tokens, rep.Limit)
	}
}

func TestBuildFixKeepsFailedSQLOnce(t *testing.T) {
	sql := "select * from users"
	msgs, _ := BuildFix(ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql"},
		ActiveSQL:  sql,
		Selection:  sql,
	}, sql, "unknown column", Budget{})
	body := msgs[1].Content
	if n := strings.Count(body, sql); n != 1 {
		t.Fatalf("failed SQL should appear once, got %d:\n%s", n, body)
	}
}
