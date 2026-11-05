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

// 结果网格的列宽：按值自适应，带下限与硬上限。
const (
	rowHeaderWidth = 56  // 行号列固定宽
	minColumnWidth = 56  // 每列下限，避免空列挤成缝
	maxColumnWidth = 320 // 每列上限（同时限制手动拖拽）
	charWidth      = 8   // 一个半角字符的估算宽度（DIP）
	cellPad        = 24  // 单元格左右内边距
)

func (a *root) resultTable(c *ui.Context, r *engine.QueryResult) {
	widths := a.columnWidths(r)
	cols := make([]ui.TableColumn, 0, len(r.Columns)+1)
	cols = append(cols, ui.TableColumn{Title: "#", ID: "#", Width: rowHeaderWidth, Fixed: true, Align: ui.End})
	for i, name := range r.Columns {
		cols = append(cols, ui.TableColumn{
			Title:    name,
			ID:       name,
			Width:    widths[i],
			MinWidth: minColumnWidth,
			MaxWidth: maxColumnWidth,
		})
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

// columnWidths 返回结果各列的宽度（按值自适应，带缓存：同一结果只量一次）。
func (a *root) columnWidths(r *engine.QueryResult) []float32 {
	if a.widthsFor == r && len(a.widths) == len(r.Columns) {
		return a.widths
	}
	a.widthsFor = r
	a.widths = resultWidths(r)
	return a.widths
}

// resultWidths 按表头与每一行渲染后的文本量出各列宽度。
func resultWidths(r *engine.QueryResult) []float32 {
	out := make([]float32, len(r.Columns))
	for i := range r.Columns {
		units := displayWidth(r.Columns[i])
		for _, row := range r.Rows {
			if i >= len(row) {
				continue
			}
			if u := displayWidth(cellText(row[i])); u > units {
				units = u
			}
		}
		w := float32(units*charWidth + cellPad)
		out[i] = min(max(w, minColumnWidth), maxColumnWidth)
	}
	return out
}

// displayWidth 估算字符串的显示宽度：半角 1 个单位，中日韩/全角 2 个单位。
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case r == 0 || r == '\n' || r == '\r' || r == '\t':
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x1100 && (r <= 0x115f || // Hangul Jamo
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) || // CJK 部首/假名/汉字
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul 音节
		(r >= 0xf900 && r <= 0xfaff) || // CJK 兼容汉字
		(r >= 0xfe30 && r <= 0xfe6f) || // CJK 兼容形式
		(r >= 0xff00 && r <= 0xff60) || // 全角 ASCII
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f9ff) || // emoji
		(r >= 0x20000 && r <= 0x3fffd)): // CJK 扩展
		return 2
	}
	return 1
}
