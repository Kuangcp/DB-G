package ui

import (
	"sort"
	"strings"
	"sync"

	myeditor "github.com/egoist/mygo/ui/editor"
)

// SQL 词表：关键字/类型/常量抄自 mygo ui/editor 的 SQL 词法表，函数为常用补充。
// 每个词同时给出大写与小写两种形式（SQL 大小写不敏感，用户两种都可能敲）。
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

var (
	sqlWordsOnce sync.Once
	sqlWords     []myeditor.Completion
)

// sqlWordItems 返回词表候选（已排序、已去重、大小写双形式）；共享只读，调用方不得修改。
func sqlWordItems() []myeditor.Completion {
	sqlWordsOnce.Do(func() {
		seen := map[string]bool{}
		out := make([]myeditor.Completion, 0, 400)
		add := func(kind myeditor.CompletionKind, words string) {
			for _, w := range strings.Fields(words) {
				for _, form := range []string{strings.ToUpper(w), strings.ToLower(w)} {
					if seen[form] {
						continue
					}
					seen[form] = true
					out = append(out, myeditor.Completion{Label: form, Kind: kind})
				}
			}
		}
		add(myeditor.CompletionKeyword, sqlKeywordWords)
		add(myeditor.CompletionType, sqlTypeWords)
		add(myeditor.CompletionConstant, sqlConstWords)
		add(myeditor.CompletionFunction, sqlFuncWords)
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
		sqlWords = out
	})
	return sqlWords
}
