package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ConsoleFiles 管理控制台正文文件（<Dir>/<consoleID>.sql）。
type ConsoleFiles struct {
	Dir string
}

// TextSink 是 ConsoleManager 对正文文件的依赖面。
type TextSink interface {
	Read(id string) (string, error)
	Write(id, text string) error
	Delete(id string) error
	Exists(id string) bool
}

// Path 返回控制台正文文件路径。
func (f ConsoleFiles) Path(id string) string {
	return filepath.Join(f.Dir, id+".sql")
}

// Read 读取正文；文件不存在（首次/被外部删除）返回空串。
func (f ConsoleFiles) Read(id string) (string, error) {
	data, err := os.ReadFile(f.Path(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Write 原子写入：同目录临时文件 + rename，跨设备等无法 rename 时回落普通写。
func (f ConsoleFiles) Write(id, text string) error {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	path := f.Path(id)
	tmp, err := os.CreateTemp(f.Dir, "."+id+".*.tmp")
	if err != nil {
		return os.WriteFile(path, []byte(text), 0o600)
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
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return os.WriteFile(path, []byte(text), 0o600)
	}
	return nil
}

// Delete 删除正文文件；不存在不算错。
func (f ConsoleFiles) Delete(id string) error {
	if err := os.Remove(f.Path(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Exists 报告正文文件是否存在且是普通文件。
func (f ConsoleFiles) Exists(id string) bool {
	fi, err := os.Stat(f.Path(id))
	return err == nil && !fi.IsDir()
}
