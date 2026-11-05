package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// formatCellValue 返回查看器里的文本：JSON 美化（仅当原值是合法 JSON 对象/数组），
// 否则返回原文本并 isJSON=false。
func formatCellValue(v any) (string, bool) {
	text := cellString(v)
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

// rowsTSV 把选中的行与列拼成 TSV（rowIdx 为结果行下标顺序，colsIdx 为数据列下标）。
func rowsTSV(r *engine.QueryResult, rowIdx, colsIdx []int, header bool) string {
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
				fields[i] = tsvField(cellString(r.Rows[row][c]))
			} else {
				fields[i] = "NULL"
			}
		}
		b.WriteString(strings.Join(fields, "\t"))
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// quoteIdentFor 按数据源方言引用标识符。
func quoteIdentFor(kind, s string) string {
	if kind == "mysql" {
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// quoteValue 把值渲染成 SQL 字面量（复制 INSERT 用，宁可不复制也不拼坏）。
func quoteValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	case []byte:
		if isTextBytes(t) {
			return quoteValue(string(t))
		}
		return "NULL" // 二进制不生成 INSERT
	case bool:
		if t {
			return "1"
		}
		return "0"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(t)
	case float32, float64:
		return strconv.FormatFloat(toFloat(t), 'g', -1, 64)
	case time.Time:
		return quoteValue(t.Format(time.RFC3339Nano))
	default:
		return quoteValue(fmt.Sprint(t))
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float32:
		return float64(t)
	case float64:
		return t
	}
	return 0
}

// insertSQL 生成「复制本行 → INSERT」语句。
func insertSQL(kind, table string, cols []string, row []any) string {
	quoted := make([]string, len(cols))
	values := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteIdentFor(kind, c)
		if i < len(row) {
			values[i] = quoteValue(row[i])
		} else {
			values[i] = "NULL"
		}
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		quoteIdentFor(kind, table), strings.Join(quoted, ", "), strings.Join(values, ", "))
}
