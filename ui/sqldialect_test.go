package ui

import (
	"testing"

	myeditor "github.com/egoist/mygo/ui/editor"
)

func wordKinds(items []myeditor.Completion) map[string]myeditor.CompletionKind {
	out := make(map[string]myeditor.CompletionKind, len(items))
	for _, it := range items {
		out[it.Label] = it.Kind
	}
	return out
}

func TestDialectWordsFollowDataSource(t *testing.T) {
	mysql := wordKinds(sqlWordItemsForKind("mysql"))
	if mysql["AUTO_INCREMENT"] != myeditor.CompletionKeyword || mysql["auto_increment"] != myeditor.CompletionKeyword {
		t.Fatalf("MySQL AUTO_INCREMENT 未进入补全: %v", mysql["AUTO_INCREMENT"])
	}
	if mysql["MEDIUMINT"] != myeditor.CompletionType {
		t.Fatalf("MySQL MEDIUMINT 应为类型: %v", mysql["MEDIUMINT"])
	}
	if mysql["GROUP_CONCAT"] != myeditor.CompletionFunction {
		t.Fatalf("MySQL GROUP_CONCAT 应为函数: %v", mysql["GROUP_CONCAT"])
	}
	if _, ok := wordKinds(sqlWordItemsForKind("sqlite"))["AUTO_INCREMENT"]; ok {
		t.Fatal("SQLite 不该出现 MySQL 专属关键字")
	}
	if wordKinds(sqlWordItemsForKind("postgres"))["RETURNING"] != myeditor.CompletionKeyword {
		t.Fatal("PostgreSQL RETURNING 未进入补全")
	}
	if wordKinds(sqlWordItemsForKind("clickhouse"))["LOWCARDINALITY"] != myeditor.CompletionType {
		t.Fatal("ClickHouse LowCardinality 未进入补全")
	}
}

func TestDialectHighlightTagsKeyword(t *testing.T) {
	lang := sqlLanguageForKind("mysql")
	if lang == nil || lang.Tokenizer == nil {
		t.Fatal("MySQL 语言不可用")
	}
	line := "id INT AUTO_INCREMENT"
	tokens, _ := lang.Tokenizer.Tokenize(line, 0, nil)
	prev := 0
	found := false
	for _, tok := range tokens {
		word := line[prev:tok.End]
		prev = tok.End
		if word == "AUTO_INCREMENT" {
			found = true
			if tok.Kind != myeditor.TokenKeyword {
				t.Fatalf("AUTO_INCREMENT 高亮类型 = %d, want keyword", tok.Kind)
			}
		}
	}
	if !found {
		t.Fatalf("没有切出 AUTO_INCREMENT: %+v", tokens)
	}
	if generic := sqlLanguageForKind(""); generic == nil || generic.Tokenizer == nil {
		t.Fatal("通用 SQL 语言不可用")
	}
}
