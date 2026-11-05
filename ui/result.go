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
		case a.currentRuntime().Running():
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "执行中…")
				if ui.Button(c, "停止").Clicked() {
					a.currentRuntime().Cancel()
				}
			})
		case a.currentRuntime().Err() != "":
			ui.Text(c, "执行失败").Bold()
			ui.Text(c, a.currentRuntime().Err()).TextColor(c.Theme().Danger).Font("monospace")
		case a.currentRuntime().Result() != nil:
			r := a.currentRuntime().Result()
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
		// []byte 既可能是 MySQL 的 TEXT/VARCHAR（应渲染文本），也可能是真正的二进制
		// （BLOB）。合法 UTF-8 且无 NUL 时按文本渲染，否则给字节数摘要。
		if isTextBytes(t) {
			return cutRunes(string(t), 512)
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return cutRunes(t, 512)
	default:
		return fmt.Sprint(t)
	}
}

// isTextBytes 判断字节串是否可以当文本显示：合法 UTF-8，且不含 NUL 等控制字符
// （制表/换行除外，它们由表格单行渲染处理）。
func isTextBytes(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == 0 {
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
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
