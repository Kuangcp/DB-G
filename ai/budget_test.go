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

func TestBuildChatIncludesDialogueInOrder(t *testing.T) {
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "mysql"},
		Dialogue: []Message{
			{Role: RoleUser, Content: "上一问"},
			{Role: RoleAssistant, Content: "上一答"},
		},
	}
	msgs, _ := BuildChat(snapshot, "这一问", Budget{})
	if len(msgs) != 4 {
		t.Fatalf("messages: %+v", msgs)
	}
	want := []struct {
		role Role
		text string
	}{
		{RoleSystem, ""},
		{RoleUser, "上一问"},
		{RoleAssistant, "上一答"},
		{RoleUser, "这一问"},
	}
	for i, w := range want {
		if msgs[i].Role != w.role || (w.text != "" && !strings.Contains(msgs[i].Content, w.text)) {
			t.Fatalf("message %d: %+v", i, msgs[i])
		}
	}
	// 一次性任务（修复/计划/改写）不能带聊天历史。
	fixMsgs, _ := BuildFix(snapshot, "select 1", "err", Budget{})
	if strings.Contains(fixMsgs[1].Content, "上一问") {
		t.Fatalf("fix prompt must not carry dialogue:\n%s", fixMsgs[1].Content)
	}
}

func TestBudgetDropsOldestDialogue(t *testing.T) {
	var dialogue []Message
	for i := range 8 {
		dialogue = append(dialogue, Message{Role: RoleUser, Content: strings.Repeat("旧消息 ", 120) + itoa(i)})
	}
	_, rep := BuildChat(ContextSnapshot{
		Connection: ConnInfo{Kind: "sqlite"},
		Dialogue:   dialogue,
	}, "新问题", Budget{ContextWindow: 2400, MaxOutput: 900})
	if rep.DroppedDialogue == 0 {
		t.Fatalf("expected old dialogue to be dropped: %+v", rep)
	}
	if rep.Dialogue == 0 || rep.Dialogue >= len(dialogue) {
		t.Fatalf("should keep the newest dialogue: %+v", rep)
	}
	if rep.Tokens > rep.Limit {
		t.Fatalf("over budget: %d > %d", rep.Tokens, rep.Limit)
	}
}

func TestBuildChatIncludesResultPlanAndDDL(t *testing.T) {
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "postgres", Database: "public"},
		ActiveSQL:  "select id from users",
		Tables: []TableContext{{
			Name: "users", DDL: "CREATE TABLE users (id bigint)",
		}},
		Result: &ResultContext{
			Columns:  []ResultColumn{{Name: "id", Type: "bigint"}},
			RowCount: 2, ExecMillis: 12, HasMore: true,
			Sample: [][]string{{"1"}, {"2"}},
		},
		Plan: &PlanContext{SQL: "select id from users", Analyze: true, Text: "Seq Scan on users"},
	}
	msgs, rep := BuildChat(snapshot, "解释一下", Budget{})
	body := msgs[1].Content
	for _, want := range []string{"当前结果集", "列: id bigint", "当前页行数: 2", "执行计划（EXPLAIN ANALYZE）",
		"Seq Scan on users", "DDL:", "CREATE TABLE users"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q:\n%s", want, body)
		}
	}
	if rep.DDL != 1 || rep.ResultRows != 2 || rep.ResultColumns != 1 || !rep.HasPlan {
		t.Fatalf("report: %+v", rep)
	}
}

func TestBudgetDropsDDLBeforeSchema(t *testing.T) {
	ddl := strings.Repeat("CREATE TABLE x (id bigint);\n", 200)
	snapshot := ContextSnapshot{
		Connection: ConnInfo{Kind: "postgres"},
		Tables: []TableContext{
			{Name: "users", Columns: []ColumnContext{{Name: "id", Type: "bigint"}}, DDL: ddl},
			{Name: "orders", Columns: []ColumnContext{{Name: "id", Type: "bigint"}}},
		},
		Result: &ResultContext{
			Columns: []ResultColumn{{Name: "id", Type: "bigint"}}, RowCount: 1,
			Sample: [][]string{{"1"}},
		},
	}
	_, rep := BuildChat(snapshot, "查 users", Budget{ContextWindow: 2400, MaxOutput: 900})
	if rep.Trimmed() == false || rep.DroppedSamples == 0 {
		t.Fatalf("expected samples trimmed first: %+v", rep)
	}
	if rep.DDL != 0 || rep.DroppedDDL == 0 {
		t.Fatalf("DDL should be trimmed under budget: %+v", rep)
	}
	if rep.Tables == 0 || rep.Columns == 0 {
		t.Fatalf("relevant schema should survive: %+v", rep)
	}
	if rep.Tokens > rep.Limit {
		t.Fatalf("over budget: %d > %d", rep.Tokens, rep.Limit)
	}
}
