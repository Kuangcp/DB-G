package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"db-g/app"
	"db-g/engine"
)

// cellString 返回单元格的完整文本（复制/查看用，不截断）；二进制给字节数摘要。
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if isTextBytes(t) {
			return string(t)
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	case string:
		return t
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// resultColumnType 返回结果里某列的数据库类型名；驱动未提供时为空。
func resultColumnType(r *engine.QueryResult, col int) string {
	if r == nil || col < 0 || col >= len(r.ColumnTypes) {
		return ""
	}
	return r.ColumnTypes[col]
}

// cellStringFor 返回某列值的完整文本。MySQL 的 DATETIME/TIMESTAMP 用
// YYYY-MM-DD HH:MM:SS[.ffffff]，方便复制出去直接写 SQL；其它列保持原样。
func cellStringFor(kind string, r *engine.QueryResult, col int, v any) string {
	if kind == "mysql" && app.MySQLDateTimeType(resultColumnType(r, col)) {
		if t, ok := v.(time.Time); ok {
			return app.MySQLDateTimeText(t)
		}
	}
	return cellString(v)
}

// 表格单元格只渲染前 512 个字符：列宽最多 320 DIP，长文本必须截断，
// 否则 mygo 每帧都要排版整段大字段；复制/查看器仍取全文。
const (
	displayCellRunes = 512
	displayCellBytes = displayCellRunes * 4 // UTF-8 单字符最多 4 字节
)

// cellTextDisplay 是表格单元格的截断文本；对 []byte 只检查/转换前缀，
// 避免每帧对 1MB 的 TEXT/BLOB 做完整 UTF-8 校验与排版。
func cellTextDisplay(v any) string {
	switch t := v.(type) {
	case string:
		return cutRunes(t, displayCellRunes)
	case []byte:
		pre := t
		if len(pre) > displayCellBytes {
			pre = pre[:displayCellBytes]
			// 前缀可能切在多字节字符中间，回退最多 3 字节让它成为合法 UTF-8。
			for i := 0; i < utf8.UTFMax-1 && len(pre) > 0 && !utf8.Valid(pre); i++ {
				pre = pre[:len(pre)-1]
			}
		}
		if isTextBytes(pre) {
			s := cutRunes(string(pre), displayCellRunes)
			if len(t) > len(pre) && !strings.HasSuffix(s, "…") {
				s += "…"
			}
			return s
		}
		return fmt.Sprintf("[%d bytes]", len(t))
	default:
		return cellText(v)
	}
}

// cellTextFor 是表格单元格的展示文本：MySQL DATETIME/TIMESTAMP 用
// YYYY-MM-DD HH:MM:SS[.ffffff]，其余列只渲染前 512 个字符。
func cellTextFor(kind string, r *engine.QueryResult, col int, v any) string {
	if kind == "mysql" && app.MySQLDateTimeType(resultColumnType(r, col)) {
		if t, ok := v.(time.Time); ok {
			return app.MySQLDateTimeText(t)
		}
	}
	return cellTextDisplay(v)
}

// formatCellText 返回查看器里的文本：JSON 美化（仅当文本是合法 JSON 对象/数组），
// 否则返回原文本并 isJSON=false。
func formatCellText(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return text, false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return text, false
	}
	return buf.String(), true
}

// tsvField 按 Excel/表格软件的约定给含制表符、换行或引号的字段加引号。
func tsvField(s string) string {
	if !strings.ContainsAny(s, "\t\n\r\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// rowsTSVFor 把选中的行与列拼成 TSV（rowIdx 为结果行下标顺序，colsIdx 为数据列下标）。
func rowsTSVFor(kind string, r *engine.QueryResult, rowIdx, colsIdx []int, header bool) string {
	var b strings.Builder
	if header && len(colsIdx) > 0 {
		heads := make([]string, len(colsIdx))
		for i, c := range colsIdx {
			if c >= 0 && c < len(r.Columns) {
				heads[i] = tsvField(r.Columns[c])
			}
		}
		b.WriteString(strings.Join(heads, "\t"))
		b.WriteByte('\n')
	}
	for _, row := range rowIdx {
		if row < 0 || row >= len(r.Rows) {
			continue
		}
		fields := make([]string, len(colsIdx))
		for i, c := range colsIdx {
			if c >= 0 && c < len(r.Rows[row]) {
				fields[i] = tsvField(cellStringFor(kind, r, c, r.Rows[row][c]))
			} else {
				fields[i] = "NULL"
			}
		}
		b.WriteString(strings.Join(fields, "\t"))
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// insertSQLFor 生成「复制本行 → INSERT」语句（与导出共用同一套转义）。
// types 与 cols 对齐，MySQL DATETIME/TIMESTAMP 用 SQL 友好字面量。
func insertSQLFor(kind, table string, cols, types []string, row []any) string {
	return app.InsertStatementFor(kind, table, cols, types, row)
}
