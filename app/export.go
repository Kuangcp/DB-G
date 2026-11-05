package app

import (
	"bufio"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// QuoteIdent 按数据源方言引用标识符（mysql 反引号，其余双引号）。
func QuoteIdent(kind, name string) string {
	if kind == "mysql" {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// QuoteValue 把值渲染成 SQL 字面量；二进制给 NULL（宁可不导出也不拼坏）。
func QuoteValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	case []byte:
		if utf8.Valid(t) && !hasControl(t) {
			return QuoteValue(string(t))
		}
		return "NULL"
	case bool:
		if t {
			return "1"
		}
		return "0"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(t)
	case float32:
		return fmt.Sprintf("%v", t)
	case float64:
		return fmt.Sprintf("%v", t)
	case time.Time:
		return QuoteValue(t.Format(time.RFC3339Nano))
	default:
		return QuoteValue(fmt.Sprint(t))
	}
}

// InsertStatement 生成一条 INSERT。
func InsertStatement(kind, table string, cols []string, row []any) string {
	quoted := make([]string, len(cols))
	values := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = QuoteIdent(kind, c)
		if i < len(row) {
			values[i] = QuoteValue(row[i])
		} else {
			values[i] = "NULL"
		}
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		QuoteIdent(kind, table), strings.Join(quoted, ", "), strings.Join(values, ", "))
}

func hasControl(b []byte) bool {
	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return true
		}
	}
	return false
}

// exportText 是导出用的文本：文本字节为原文，二进制给 base64。
func exportText(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if utf8.Valid(t) && !hasControl(t) {
			return string(t)
		}
		return "base64:" + base64.StdEncoding.EncodeToString(t)
	case string:
		return t
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// jsonValue 把单元格转成 JSON 友好的值。
func jsonValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(t) && !hasControl(t) {
			return string(t)
		}
		return "base64:" + base64.StdEncoding.EncodeToString(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return t
	}
}

// ExportWriters 是一组流式写出器：先 Start 列名，再逐行 Row，最后 Close。
type ExportWriters interface {
	Start(cols []string) error
	Row(row []any) error
	Close() error
}

// NewExportWriter 按格式创建写出器（csv/json/insert）。
func NewExportWriter(w io.Writer, format, kind, table string) ExportWriters {
	bw := bufio.NewWriter(w)
	switch format {
	case "json":
		return &jsonWriter{w: bw}
	case "insert":
		return &insertWriter{w: bw, kind: kind, table: table}
	default:
		return &csvWriter{w: csv.NewWriter(bw), buf: bw}
	}
}

type csvWriter struct {
	w   *csv.Writer
	buf *bufio.Writer
}

func (c *csvWriter) Start(cols []string) error { return c.w.Write(cols) }
func (c *csvWriter) Row(row []any) error {
	rec := make([]string, len(row))
	for i, v := range row {
		rec[i] = exportText(v)
	}
	return c.w.Write(rec)
}
func (c *csvWriter) Close() error {
	c.w.Flush()
	if err := c.w.Error(); err != nil {
		return err
	}
	return c.buf.Flush()
}

type jsonWriter struct {
	w     *bufio.Writer
	cols  []string
	first bool
	init  bool
}

func (j *jsonWriter) Start(cols []string) error {
	j.cols, j.first = cols, true
	_, err := j.w.WriteString("[\n")
	return err
}
func (j *jsonWriter) Row(row []any) error {
	m := make(map[string]any, len(j.cols))
	for i, c := range j.cols {
		if i < len(row) {
			m[c] = jsonValue(row[i])
		} else {
			m[c] = nil
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if !j.first {
		if _, err := j.w.WriteString(",\n"); err != nil {
			return err
		}
	}
	j.first = false
	_, err = j.w.Write(data)
	return err
}
func (j *jsonWriter) Close() error {
	if _, err := j.w.WriteString("\n]\n"); err != nil {
		return err
	}
	return j.w.Flush()
}

type insertWriter struct {
	w     *bufio.Writer
	kind  string
	table string
	cols  []string
}

func (i *insertWriter) Start(cols []string) error { i.cols = cols; return nil }
func (i *insertWriter) Row(row []any) error {
	_, err := i.w.WriteString(InsertStatement(i.kind, i.table, i.cols, row) + "\n")
	return err
}
func (i *insertWriter) Close() error { return i.w.Flush() }

// ExportRows 把已加载的结果写到文件（format: csv/json/insert）。
func ExportRows(path, format, kind, table string, cols []string, rows [][]any) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := NewExportWriter(f, format, kind, table)
	if err := w.Start(cols); err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := w.Row(row); err != nil {
			return 0, err
		}
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// StreamFn 是 RowStreamer 的调用形态（便于测试注入）。
type StreamFn func(onColumns func([]string) error, onRow func([]any) error) error

// ExportStream 全量流式导出：边读边写，内存只留当前行。
func ExportStream(path, format, kind, table string, stream StreamFn) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := NewExportWriter(f, format, kind, table)
	n := 0
	err = stream(func(cols []string) error { return w.Start(cols) }, func(row []any) error {
		n++
		return w.Row(row)
	})
	if err != nil {
		return n, err
	}
	return n, w.Close()
}
