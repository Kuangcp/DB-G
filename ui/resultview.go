package ui

import (
	"fmt"
	"sort"
	"strings"

	"db-g/app"
	"db-g/engine"
)

// fetchMoreSQL 把一条 SELECT/WITH 语句包成派生表再分页；不是单条查询时返回 false。
func fetchMoreSQL(sql string, offset, limit int) (string, bool) {
	stmts := app.SplitStatements(sql)
	if len(stmts) != 1 {
		return "", false
	}
	body := strings.TrimSpace(sql[stmts[0].Start:stmts[0].End])
	head := strings.ToUpper(firstWord(body))
	if head != "SELECT" && head != "WITH" {
		return "", false
	}
	return fmt.Sprintf("SELECT * FROM (%s) AS dbg LIMIT %d OFFSET %d", body, limit, offset), true
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s); i++ {
		if !isIdentPart(s[i]) {
			return s[:i]
		}
	}
	return s
}

// resultView 是结果区的纯视图状态：排序与每列过滤（不重跑 SQL）。
type resultView struct {
	sortCol  int
	sortDesc bool
	filters  map[int]string
	order    []int
	key      string
}

func newResultView() *resultView {
	return &resultView{sortCol: -1, filters: map[int]string{}}
}

func (v *resultView) sortBy(r *engine.QueryResult, col int, desc bool) {
	v.sortCol, v.sortDesc = col, desc
	v.key = ""
	_ = r
}

func (v *resultView) setFilter(col int, text string) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		delete(v.filters, col)
	} else {
		v.filters[col] = text
	}
	v.key = ""
}

func (v *resultView) clearFilter(col int) {
	delete(v.filters, col)
	v.key = ""
}

func (v *resultView) clear() {
	v.sortCol, v.sortDesc, v.key = -1, false, ""
	v.filters = map[int]string{}
}

// apply 返回要显示的结果行下标：先过滤，再按排序列排序（NULL 最后）。
func (v *resultView) apply(r *engine.QueryResult) []int {
	if r == nil {
		v.order = nil
		return nil
	}
	key := fmt.Sprintf("%p|%d|%d|%t|%v", r, len(r.Rows), v.sortCol, v.sortDesc, v.filters)
	if key == v.key && len(v.order) == countVisible(r, v.filters) {
		return v.order
	}
	out := make([]int, 0, len(r.Rows))
	for i, row := range r.Rows {
		if matchesFilters(row, v.filters) {
			out = append(out, i)
		}
	}
	if v.sortCol >= 0 && v.sortCol < len(r.Columns) && len(out) > 1 {
		col := v.sortCol
		desc := v.sortDesc
		sort.SliceStable(out, func(a, b int) bool {
			av, bv := cellAt(r.Rows[out[a]], col), cellAt(r.Rows[out[b]], col)
			// NULL 无论升降序都排在最后（与常见数据库客户端一致）。
			if av == nil || bv == nil {
				return bv == nil && av != nil
			}
			c := compareCells(av, bv)
			if desc {
				c = -c
			}
			return c < 0
		})
	}
	v.order, v.key = out, key
	return out
}

func countVisible(r *engine.QueryResult, filters map[int]string) int {
	n := 0
	for _, row := range r.Rows {
		if matchesFilters(row, filters) {
			n++
		}
	}
	return n
}

func matchesFilters(row []any, filters map[int]string) bool {
	for col, want := range filters {
		got := strings.ToLower(cellString(cellAt(row, col)))
		if !strings.Contains(got, want) {
			return false
		}
	}
	return true
}

func cellAt(row []any, col int) any {
	if col < 0 || col >= len(row) {
		return nil
	}
	return row[col]
}

// compareCells 比较两个单元格：NULL 最后；数字按值；其它按文本。
func compareCells(a, b any) int {
	aNil, bNil := a == nil, b == nil
	switch {
	case aNil && bNil:
		return 0
	case aNil:
		return 1
	case bNil:
		return -1
	}
	if af, aok := asFloat(a); aok {
		if bf, bok := asFloat(b); bok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(cellString(a), cellString(b))
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case float32:
		return float64(t), true
	case float64:
		return t, true
	}
	return 0, false
}
