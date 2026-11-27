package sql

import (
	"sort"
	"strings"
	"sync"
)

// WordKind 是补全/高亮用的词类型；与 mygo editor 的具体类型解耦。
type WordKind int

const (
	WordKeyword WordKind = iota
	WordType
	WordConstant
	WordFunction
)

// Word 是词表里的一个词条。
type Word struct {
	Text string
	Kind WordKind
}

// 通用 SQL 词表：关键字/类型/常量/常用函数。
var (
	sqlKeywordWords = `select from where and or not insert into values update set delete create table drop alter truncate merge call use show describe
		add column index view as on join left right inner outer full cross group by order having limit offset
		union all distinct case when then else end is null in exists between like primary key foreign references
		default unique check constraint begin commit rollback transaction if returning with recursive asc desc
		trigger function procedure replace temporary temp database schema grant revoke cascade conflict do
		nothing using natural explain analyze pragma vacuum`
	sqlTypeWords = `int integer bigint smallint tinyint real float double decimal numeric text varchar char
		boolean bool date time timestamp blob json jsonb uuid serial`
	sqlConstWords = `true false null`
	sqlFuncWords  = `count sum avg min max coalesce ifnull nullif now date time length lower upper substr
		replace round abs cast concat trim ltrim rtrim`
)

type dialectWords struct {
	keywords  []string
	types     []string
	functions []string
}

var sqlDialects = map[string]dialectWords{
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
		types:     []string{"blob", "integer", "real", "text", "numeric"},
		functions: []string{"typeof", "instr", "printf", "last_insert_rowid", "changes", "total_changes", "date", "time", "datetime", "julianday", "strftime", "substr"},
	},
}

var (
	wordsOnce sync.Once
	baseWords []Word

	dialectOnce  sync.Map // kind → map[string]WordKind
	dialectTerms sync.Map // kind → []Word
)

// Words 返回通用词表（大小写双形式、去重、按文本排序）；返回值只读。
func Words() []Word {
	wordsOnce.Do(func() {
		seen := map[string]bool{}
		var out []Word
		add := func(kind WordKind, words string) {
			for _, w := range strings.Fields(words) {
				for _, form := range []string{strings.ToUpper(w), strings.ToLower(w)} {
					if seen[form] {
						continue
					}
					seen[form] = true
					out = append(out, Word{Text: form, Kind: kind})
				}
			}
		}
		add(WordKeyword, sqlKeywordWords)
		add(WordType, sqlTypeWords)
		add(WordConstant, sqlConstWords)
		add(WordFunction, sqlFuncWords)
		sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
		baseWords = out
	})
	return baseWords
}

// DialectTermMap 返回某数据源专属词（小写 → 类型）；没有专属词时返回空 map。
func DialectTermMap(kind string) map[string]WordKind {
	if v, ok := dialectOnce.Load(kind); ok {
		return v.(map[string]WordKind)
	}
	d, ok := sqlDialects[kind]
	if !ok {
		empty := map[string]WordKind{}
		dialectOnce.Store(kind, empty)
		return empty
	}
	out := make(map[string]WordKind, len(d.keywords)+len(d.types)+len(d.functions))
	for _, w := range d.keywords {
		out[strings.ToLower(w)] = WordKeyword
	}
	for _, w := range d.types {
		out[strings.ToLower(w)] = WordType
	}
	for _, w := range d.functions {
		out[strings.ToLower(w)] = WordFunction
	}
	dialectOnce.Store(kind, out)
	return out
}

// DialectTerms 返回通用词表 + 某数据源专属词（大小写双形式、去重、排序）；返回值只读。
func DialectTerms(kind string) []Word {
	if kind == "" {
		return Words()
	}
	if v, ok := dialectTerms.Load(kind); ok {
		return v.([]Word)
	}
	base := Words()
	terms := DialectTermMap(kind)
	if len(terms) == 0 {
		dialectTerms.Store(kind, base)
		return base
	}
	seen := make(map[string]bool, len(base)+len(terms)*2)
	out := make([]Word, 0, len(base)+len(terms)*2)
	for _, w := range base {
		seen[w.Text] = true
		out = append(out, w)
	}
	for w, kind := range terms {
		for _, form := range []string{strings.ToUpper(w), w} {
			if seen[form] {
				continue
			}
			seen[form] = true
			out = append(out, Word{Text: form, Kind: kind})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	dialectTerms.Store(kind, out)
	return out
}

// IsExtraKeyword 报告 mygo 内置 SQL 词表里缺少、但编辑器高亮应该识别为关键字的整词。
func IsExtraKeyword(s string) bool {
	switch len(s) {
	case 3:
		return strings.EqualFold(s, "use")
	case 4:
		return strings.EqualFold(s, "call") || strings.EqualFold(s, "show") ||
			strings.EqualFold(s, "desc")
	case 5:
		return strings.EqualFold(s, "merge")
	case 8:
		return strings.EqualFold(s, "truncate") || strings.EqualFold(s, "describe")
	}
	return false
}

var (
	sqlWordSetOnce sync.Once
	sqlWordSet     map[string]bool
)

// IsSQLWord 报告 word 是否属于通用 SQL 词表（大小写不敏感）。
func IsSQLWord(word string) bool {
	sqlWordSetOnce.Do(func() {
		sqlWordSet = map[string]bool{}
		for _, w := range Words() {
			sqlWordSet[strings.ToLower(w.Text)] = true
		}
	})
	return sqlWordSet[strings.ToLower(word)]
}
