package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// IsSQLFile 报告路径是否以 .sql 结尾（大小写不敏感）。
func IsSQLFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".sql")
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

// ReadExternalSQL 读取 UTF-8 SQL 文本；去掉 UTF-8 BOM，非法 UTF-8 直接报错。
func ReadExternalSQL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("文件不是 UTF-8 文本")
	}
	return strings.TrimPrefix(string(data), "\ufeff"), nil
}

// WriteExternalSQL 原子写回外部文件，保留原权限（新文件 0644）。
// 不创建父目录：路径失效时让调用方显式处理。
func WriteExternalSQL(path, text string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".db-g-sql-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(text); err != nil {
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
