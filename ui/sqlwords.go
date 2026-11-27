package ui

import (
	"sort"
	"strings"
	"sync"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/locales"
	"db-g/sql"
)

// sqlWordItems 返回通用词表候选（已排序、已去重、大小写双形式）。
func sqlWordItems() []myeditor.Completion {
	return completionItems(sql.Words(), false)
}

var (
	sqlWordCacheMu sync.Mutex
	sqlWordCache   = map[string][]myeditor.Completion{}
)

// sqlWordItemsForKind 返回通用词表 + 该数据源的专属关键字/类型/函数。
func sqlWordItemsForKind(kind string) []myeditor.Completion {
	base := sqlWordItems()
	terms := sql.DialectTermMap(kind)
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
	for w, k := range terms {
		for _, form := range []string{strings.ToUpper(w), w} {
			if seen[form] {
				continue
			}
			seen[form] = true
			out = append(out, myeditor.Completion{
				Label:  form,
				Kind:   completionKind(k),
				Detail: completionDetail(k),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	sqlWordCache[kind] = out
	return out
}

func completionItems(words []sql.Word, withDetail bool) []myeditor.Completion {
	out := make([]myeditor.Completion, 0, len(words))
	for _, w := range words {
		item := myeditor.Completion{Label: w.Text, Kind: completionKind(w.Kind)}
		if withDetail {
			item.Detail = completionDetail(w.Kind)
		}
		out = append(out, item)
	}
	return out
}

func completionKind(k sql.WordKind) myeditor.CompletionKind {
	switch k {
	case sql.WordType:
		return myeditor.CompletionType
	case sql.WordConstant:
		return myeditor.CompletionConstant
	case sql.WordFunction:
		return myeditor.CompletionFunction
	}
	return myeditor.CompletionKeyword
}

func completionDetail(k sql.WordKind) string {
	switch k {
	case sql.WordType:
		return locales.CompletionType()
	case sql.WordFunction:
		return locales.CompletionFunction()
	}
	return locales.CompletionKeyword()
}
