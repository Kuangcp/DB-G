package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ConsoleFiles 管理控制台正文文件：内部控制台在 <Dir>/<consoleID>.sql，
// 外部控制台写在记录里绑定的用户路径（Store 提供映射）。
type ConsoleFiles struct {
	Dir   string
	Store *ConnStore
}

// External 报告控制台是否绑定用户磁盘文件（ConsoleManager 写前一致性校验用）。
func (f ConsoleFiles) External(id string) bool {
	rec, ok := f.record(id)
	return ok && rec.External
}

// TextSink 是 ConsoleManager 对正文文件的依赖面。
type TextSink interface {
	Read(id string) (string, error)
	Write(id, text string) error
	Delete(id string) error
	Exists(id string) bool
}

// record 返回控制台记录；没有 Store 时按内部控制台处理。
func (f ConsoleFiles) record(id string) (ConsoleRecord, bool) {
	if f.Store == nil {
		return ConsoleRecord{}, false
	}
	return f.Store.GetConsole(id)
}

// Path 返回控制台正文文件路径；外部控制台是用户文件本身。
func (f ConsoleFiles) Path(id string) string {
	if rec, ok := f.record(id); ok && rec.External {
		return rec.FilePath
	}
	return filepath.Join(f.Dir, id+".sql")
}

// Read 读取正文；内部文件不存在（首次/被外部删除）返回空串。
// 外部文件不存在返回 fs.ErrNotExist，让调用方能区分「空文件」与「文件没了」。
func (f ConsoleFiles) Read(id string) (string, error) {
	path := f.Path(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if rec, ok := f.record(id); ok && rec.External {
				return "", err
			}
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Write 原子写入：同目录临时文件 + rename，跨设备等无法 rename 时回落普通写。
// 外部文件保留原权限且不创建父目录（不会在用户没注意时造出目录）。
func (f ConsoleFiles) Write(id, text string) error {
	rec, external := f.record(id)
	path := f.Path(id)
	mode := os.FileMode(0o600)
	if external && rec.External {
		mode = 0o644
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
	} else if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".db-g-console-*.tmp")
	if err != nil {
		return os.WriteFile(path, []byte(text), mode)
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
		return os.WriteFile(path, []byte(text), mode)
	}
	return nil
}

// Delete 删除正文文件；外部文件属于用户，绝不删除。
func (f ConsoleFiles) Delete(id string) error {
	if rec, ok := f.record(id); ok && rec.External {
		return nil
	}
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
