package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxExternalSQLSize 是外部 .sql 文件的大小上限；超过直接拒绝，避免把编辑器拖死。
const MaxExternalSQLSize = 8 << 20 // 8 MiB

var (
	ErrExternalTooBig = fmt.Errorf("SQL 文件超过 %d MiB", MaxExternalSQLSize>>20)
	ErrNotSQLFile     = errors.New("只支持 .sql 文件")
	ErrNotTextSQL     = errors.New("不是文本 SQL 文件")
)

// IsSQLFile 报告路径是否以 .sql 结尾（大小写不敏感）。
func IsSQLFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".sql")
}

// CheckExternalSQL 校验一个可拖入/打开的 SQL 文件，返回解析后的绝对路径。
func CheckExternalSQL(path string) (string, error) {
	if !IsSQLFile(path) {
		return "", ErrNotSQLFile
	}
	norm, err := NormalizeExternalPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(norm)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("文件不存在: %s", norm)
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotTextSQL
	}
	if info.Size() > MaxExternalSQLSize {
		return "", ErrExternalTooBig
	}
	if _, err := ReadExternalSQL(norm); err != nil {
		return "", err
	}
	return norm, nil
}

// NormalizeExternalPath 返回用于去重的绝对路径；能解析符号链接时用真实路径。
func NormalizeExternalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	return filepath.Clean(abs), nil
}

// ExternalFileState 是外部文件的一致性快照。
type ExternalFileState struct {
	Exists  bool
	ModTime time.Time
	Size    int64
}

// StatExternalSQL 读取外部文件的状态（不存在时 Exists=false，不视为错误）。
func StatExternalSQL(path string) ExternalFileState {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ExternalFileState{}
	}
	return ExternalFileState{Exists: true, ModTime: info.ModTime(), Size: info.Size()}
}

// ReadExternalSQL 读取 UTF-8 SQL 文本；去掉 UTF-8 BOM。
// 超过大小上限、非 UTF-8 或含 NUL（二进制）直接报错。
func ReadExternalSQL(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > MaxExternalSQLSize {
		return "", ErrExternalTooBig
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return "", ErrNotTextSQL
	}
	return strings.TrimPrefix(string(data), "\ufeff"), nil
}

// externalFormat 是外部文件写回时需要沿用的编码细节。
type externalFormat struct {
	bom  bool // 原文件带 UTF-8 BOM
	crlf bool // 原文件以 CRLF 换行
}

// detectExternalFormat 只看文件开头，判断是否带 BOM、是否 CRLF 换行。
func detectExternalFormat(path string) externalFormat {
	f, err := os.Open(path)
	if err != nil {
		return externalFormat{}
	}
	defer f.Close()
	head := make([]byte, 64<<10)
	n, _ := io.ReadFull(f, head)
	data := head[:n]
	var format externalFormat
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		format.bom = true
		data = data[3:]
	}
	crlf := bytes.Count(data, []byte("\r\n"))
	lf := bytes.Count(data, []byte("\n"))
	format.crlf = crlf > 0 && crlf == lf
	return format
}

// encodeExternal 按原格式还原 BOM 与换行；编辑缓冲可能是 LF 混入 CRLF。
func encodeExternal(text string, format externalFormat) []byte {
	if format.crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if format.bom && !strings.HasPrefix(text, "\ufeff") {
		text = "\ufeff" + text
	}
	return []byte(text)
}

// WriteExternalSQL 原子写回外部文件，保留原权限（新文件 0644）。
// 不创建父目录：路径失效时让调用方显式处理。
func WriteExternalSQL(path, text string) error {
	mode := os.FileMode(0o644)
	format := detectExternalFormat(path)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}
	data := encodeExternal(text, format)
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".db-g-sql-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
