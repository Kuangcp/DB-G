package ui

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"db-g/engine"
)

func (a *root) resultPane(c *ui.Context) {
	ui.Column(c).Fill().Gap(6).Children(func() {
		switch {
		case a.con.Running():
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "执行中…")
				if ui.Button(c, "停止").Clicked() {
					a.con.Cancel()
				}
			})
		case a.con.Err() != "":
			ui.Text(c, "执行失败").Bold()
			ui.Text(c, a.con.Err()).TextColor(c.Theme().Danger).Font("monospace")
		case a.con.Result() != nil:
			r := a.con.Result()
			if !r.IsQuery {
				ui.Textf(c, "OK — %d 行受影响", r.Affected)
				return
			}
			if r.Truncated {
				ui.Badge(c, "结果已截断：超过 500 行或 16MB 字符预算")
			}
			a.resultTable(c, r)
		default:
			switch {
			case a.sess == nil:
				ui.Text(c, "请先连接数据源").TextColor(c.Theme().TextMuted)
			case a.selStart == a.selEnd:
				ui.Text(c, "请先选中要执行的 SQL").TextColor(c.Theme().TextMuted)
			default:
				ui.Text(c, "Ctrl+Enter 执行选中 SQL").TextColor(c.Theme().TextMuted)
			}
		}
	})
}

func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: 56, Fixed: true, Align: ui.End})
	for _, name := range r.Columns {
		cols = append(cols, ui.TableColumn{Title: name, ID: name})
	}
	ui.Table(c, &a.resultState, cols, len(r.Rows), func(row, col int) {
		if col == 0 {
			ui.Text(c, strconv.Itoa(row+1)).TextColor(c.Theme().TextMuted)
			return
		}
		ui.Text(c, cellText(r.Rows[row][col-1])).SingleLine()
	}).Grow(1)
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return cutRunes(t, 512)
	default:
		return fmt.Sprint(t)
	}
}

func cutRunes(s string, n int) string {
	i := 0
	for k := 0; k < n; k++ {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if i >= len(s) {
		return s
	}
	return s[:i] + "…"
}
