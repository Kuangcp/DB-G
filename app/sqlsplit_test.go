package app

import (
	"strings"
	"testing"
)

func slice(t *testing.T, text string, s SQLStatement) string {
	t.Helper()
	if s.Start < 0 || s.End > len(text) || s.Start > s.End {
		t.Fatalf("bad range %+v for %q", s, text)
	}
	return text[s.Start:s.End]
}

func TestSplitStatementsBasic(t *testing.T) {
	text := "select 1; select 2"
	got := SplitStatements(text)
	if len(got) != 2 {
		t.Fatalf("got %d statements: %+v", len(got), got)
	}
	if slice(t, text, got[0]) != "select 1" || slice(t, text, got[1]) != "select 2" {
		t.Fatalf("ranges: %q / %q", slice(t, text, got[0]), slice(t, text, got[1]))
	}
}

func TestSplitStatementsKeepsSemicolonInString(t *testing.T) {
	text := "select ';' as x; select 2"
	got := SplitStatements(text)
	if len(got) != 2 || slice(t, text, got[0]) != "select ';' as x" {
		t.Fatalf("got %+v", got)
	}
}

func TestSplitStatementsKeepsSemicolonInComments(t *testing.T) {
	for _, text := range []string{"-- a;\nselect 1", "/* ; */ select 1;", "select 1 /* ; */; select 2"} {
		got := SplitStatements(text)
		if len(got) != 1 && !strings.HasPrefix(text, "select 1 /*") {
			t.Fatalf("%q → %d statements", text, len(got))
		}
		if strings.HasPrefix(text, "select 1 /*") && len(got) != 2 {
			t.Fatalf("%q → %d statements", text, len(got))
		}
	}
}

func TestSplitStatementsSkipsEmptyAndCommentOnly(t *testing.T) {
	if got := SplitStatements(""); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
	if got := SplitStatements(";;;   ;;"); len(got) != 0 {
		t.Fatalf("separators only: %+v", got)
	}
	if got := SplitStatements("-- just a comment\n;"); len(got) != 0 {
		t.Fatalf("comment only: %+v", got)
	}
	got := SplitStatements(";;; select 1;;")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSplitStatementsTrimsWhitespace(t *testing.T) {
	text := "select 1;\n\n   select 2  ;\n"
	got := SplitStatements(text)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if slice(t, text, got[1]) != "select 2" {
		t.Fatalf("second = %q", slice(t, text, got[1]))
	}
}

func TestSplitStatementsTriggerBody(t *testing.T) {
	text := `CREATE TRIGGER t AFTER INSERT ON a BEGIN
	UPDATE a SET x = 1;
	UPDATE a SET y = 2;
END;
SELECT 1`
	got := SplitStatements(text)
	if len(got) != 2 {
		t.Fatalf("trigger body must not split: %d statements: %+v", len(got), got)
	}
	if !strings.HasSuffix(slice(t, text, got[0]), "END") {
		t.Fatalf("first: %q", slice(t, text, got[0]))
	}
	if slice(t, text, got[1]) != "SELECT 1" {
		t.Fatalf("second: %q", slice(t, text, got[1]))
	}
}

func TestStatementAt(t *testing.T) {
	text := "select 1; select 2"
	second := strings.Index(text, "select 2")
	st, ok := StatementAt(text, second+3)
	if !ok || slice(t, text, st) != "select 2" {
		t.Fatalf("StatementAt: %+v ok=%v", st, ok)
	}
	if _, ok := StatementAt("", 0); ok {
		t.Fatal("empty text has no statement")
	}
	endText := "select 1;"
	st, ok = StatementAt(endText, len(endText))
	if !ok || slice(t, endText, st) != "select 1" {
		t.Fatalf("cursor at end must land on the statement: %+v", st)
	}
}
