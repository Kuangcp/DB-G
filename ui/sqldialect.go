package ui

import (
	"sort"
	"strings"
	"sync"

	myeditor "github.com/egoist/mygo/ui/editor"
)

// sqlDialect 是某个数据源相对通用 SQL 多出来的词：高亮与补全都用它。
// 只收单词（标识符），多词短语仍由通用词表里的单个关键字拼出来。
type sqlDialect struct {
	keywords  []string
	types     []string
	functions []string
}

var sqlDialects = map[string]sqlDialect{
	"mysql": {
		keywords: []string{
			"auto_increment", "unsigned", "zerofill", "engine", "charset", "collate", "comment",
			"show", "databases", "tables", "columns", "processlist", "status", "variables", "warnings",
			"describe", "use", "locking", "duplicate", "low_priority", "high_priority", "ignore",
			"straight_join", "sql_calc_found_rows", "sql_no_cache", "sql_cache", "force", "keys",
			"optimize", "repair", "checksum", "flush", "privileges", "isolation", "repeatable",
			"committed", "uncommitted", "serializable", "nowait", "generated", "virtual", "stored",
			"invisible", "visible",
		},
		types: []string{
			"mediumint", "longtext", "mediumtext", "tinytext",
			"longblob", "mediumblob", "tinyblob", "datetime", "year", "enum", "set", "binary",
			"varbinary", "bit", "geometry", "point", "linestring", "polygon", "multipoint",
			"multilinestring", "multipolygon", "geometrycollection", "json",
		},
		functions: []string{
			"ifnull", "date_format", "str_to_date", "group_concat", "find_in_set", "last_insert_id",
			"unix_timestamp", "from_unixtime", "curdate", "curtime", "concat_ws", "timestampdiff",
		},
	},
	"postgres": {
		keywords: []string{
			"returning", "ilike", "similar", "materialized", "concurrently", "tablespace",
			"inherits", "owner", "extension", "language", "unlogged", "generated", "always",
			"identity", "partition", "window", "filter", "lateral", "excluded", "cluster",
			"prepare", "execute", "deallocate", "listen", "notify", "unlisten", "copy",
			"tablesample", "recursive", "only", "nowait", "share", "analyze", "vacuum",
		},
		types: []string{
			"serial", "bigserial", "smallserial", "bytea", "uuid", "jsonb", "inet", "cidr",
			"macaddr", "tsvector", "tsquery", "interval", "timestamptz", "money", "xml",
			"oid", "regclass", "int4", "int8", "float4", "float8", "smallint", "boolean",
		},
		functions: []string{
			"now", "current_date", "current_timestamp", "date_trunc", "to_char", "to_date",
			"string_agg", "array_agg", "generate_series", "coalesce", "greatest", "least",
		},
	},
	"clickhouse": {
		keywords: []string{
			"final", "sample", "prewhere", "global", "settings", "format", "ttl",
			"partition", "primary", "order", "by", "materialized", "view", "dictionary",
			"alter", "update", "delete", "optimize", "attach", "detach", "rename", "exchange",
		},
		types: []string{
			"uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64",
			"float32", "float64", "decimal", "string", "fixedstring", "uuid", "date",
			"date32", "datetime", "datetime64", "enum8", "enum16", "array", "tuple", "map",
			"nullable", "lowcardinality", "nested", "aggregatefunction", "simpleaggregatefunction",
			"ipv4", "ipv6", "json", "nothing",
		},
		functions: []string{
			"tostring", "toint32", "toint64", "todate", "todatetime", "tofloat64",
			"countif", "sumif", "avgif", "uniq", "uniqexact", "grouparray", "now", "today",
		},
	},
	"sqlite": {
		keywords: []string{
			"autoincrement", "without", "rowid", "virtual", "abort", "fail", "attach", "detach",
			"reindex", "pragma", "vacuum", "window", "over", "exclude", "ties", "groups",
			"filter", "raise", "each",
		},
		types: []string{"blob", "integer", "real", "text", "numeric"},
		functions: []string{
			"typeof", "instr", "printf", "last_insert_rowid", "changes", "total_changes",
			"date", "time", "datetime", "julianday", "strftime", "substr",
		},
	},
}

// dialectKinds 返回某数据源的词与对应的补全类型（key 为小写）。
func dialectKinds(kind string) map[string]myeditor.CompletionKind {
	d, ok := sqlDialects[kind]
	if !ok {
		return nil
	}
	out := make(map[string]myeditor.CompletionKind, len(d.keywords)+len(d.types)+len(d.functions))
	for _, w := range d.keywords {
		out[strings.ToLower(w)] = myeditor.CompletionKeyword
	}
	for _, w := range d.types {
		out[strings.ToLower(w)] = myeditor.CompletionType
	}
	for _, w := range d.functions {
		out[strings.ToLower(w)] = myeditor.CompletionFunction
	}
	return out
}

// completionTokenKind 把补全类型映射成高亮 token 类型。
func completionTokenKind(k myeditor.CompletionKind) myeditor.TokenKind {
	switch k {
	case myeditor.CompletionKeyword:
		return myeditor.TokenKeyword
	case myeditor.CompletionType:
		return myeditor.TokenType
	case myeditor.CompletionFunction:
		return myeditor.TokenFunction
	}
	return myeditor.TokenVariable
}

var (
	sqlWordCacheMu sync.Mutex
	sqlWordCache   = map[string][]myeditor.Completion{}
)

// sqlWordItemsForKind 返回通用词表 + 该数据源的专属关键字/类型/函数。
func sqlWordItemsForKind(kind string) []myeditor.Completion {
	base := sqlWordItems()
	if kind == "" {
		return base
	}
	terms := dialectKinds(kind)
	if len(terms) == 0 {
		return base
	}
	sqlWordCacheMu.Lock()
	defer sqlWordCacheMu.Unlock()
	if cached, ok := sqlWordCache[kind]; ok {
		return cached
	}
	seen := make(map[string]bool, len(base)+len(terms)*2)
	out := make([]myeditor.Completion, 0, len(base)+len(terms)*2)
	for _, it := range base {
		seen[it.Label] = true
		out = append(out, it)
	}
	for w, kind := range terms {
		for _, form := range []string{strings.ToUpper(w), w} {
			if seen[form] {
				continue
			}
			seen[form] = true
			detail := "关键字"
			if kind == myeditor.CompletionType {
				detail = "类型"
			} else if kind == myeditor.CompletionFunction {
				detail = "函数"
			}
			out = append(out, myeditor.Completion{Label: form, Detail: detail, Kind: kind})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	sqlWordCache[kind] = out
	return out
}

var (
	sqlLangMu    sync.Mutex
	sqlLangCache = map[string]*myeditor.Language{}
)

// sqlLanguageForKind 返回 SQL 语言：在 mygo 的通用词表之上把方言词重新标成
// 关键字/类型/函数，从而得到对应数据源的高亮。
func sqlLanguageForKind(kind string) *myeditor.Language {
	base := myeditor.LanguageNamed("SQL")
	terms := dialectKinds(kind)
	if base == nil || len(terms) == 0 {
		return base
	}
	sqlLangMu.Lock()
	defer sqlLangMu.Unlock()
	if cached, ok := sqlLangCache[kind]; ok {
		return cached
	}
	clone := *base
	inner := base.Tokenizer
	if inner == nil {
		sqlLangCache[kind] = &clone
		return &clone
	}
	clone.Tokenizer = myeditor.TokenizerFunc(func(line string, state uint64, tokens []myeditor.Token) ([]myeditor.Token, uint64) {
		start := len(tokens)
		out, st := inner.Tokenize(line, state, tokens)
		if start != 0 {
			return out, st
		}
		prev := 0
		for i := range out {
			word := line[prev:out[i].End]
			prev = out[i].End
			if out[i].Kind != myeditor.TokenVariable {
				continue
			}
			if k, ok := terms[strings.ToLower(word)]; ok {
				out[i].Kind = completionTokenKind(k)
			}
		}
		return out, st
	})
	sqlLangCache[kind] = &clone
	return &clone
}
