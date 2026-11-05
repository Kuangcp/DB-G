package ui

import "testing"

func TestDotIdent(t *testing.T) {
	cases := []struct {
		text   string
		offset int
		want   string
		ok     bool
	}{
		{"select * from users.", 20, "users", true},
		{"select users.", 13, "users", true},
		{"SELECT a.", 9, "a", true},
		{"select 1.", 9, "", false},      // 数字不是标识符
		{"select 'abc.", 12, "", false},  // 单引号字符串里
		{"select 1 -- x.", 14, "", false}, // 行注释里
		{"a.b.", 4, "b", true},           // 最后一个点号前是 b
		{"users.", 0, "", false},         // 光标在行首
		{"", 0, "", false},
	}
	for _, c := range cases {
		got, ok := dotIdent(c.text, c.offset)
		if ok != c.ok || got != c.want {
			t.Fatalf("dotIdent(%q,%d) = %q,%v want %q,%v", c.text, c.offset, got, ok, c.want, c.ok)
		}
	}
}

func TestDotIdentQuotedPrefix(t *testing.T) {
	// 引号标识符写作 `"tbl".`：v1 不解析（返回 false），退化为 A2 行为。
	if _, ok := dotIdent(`SELECT "tbl".`, 13); ok {
		t.Fatal("quoted identifiers are out of scope in v1")
	}
}
