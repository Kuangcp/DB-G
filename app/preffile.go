package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// loadJSONFile 读取并反序列化 JSON 配置；文件不存在返回 found=false 且不报错。
func loadJSONFile(path string, v any) (found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return true, err
	}
	return true, nil
}

// saveJSONFile 原子写 JSON 配置：先写 <path>.tmp 再改名，文件权限固定 0600。
// dirPerm 通常 0755；含密钥的同步/AI 配置用 0700。
func saveJSONFile(path string, v any, dirPerm os.FileMode) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		dir = "."
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
