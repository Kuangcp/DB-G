package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ProfileBundle 是连接档案导出文件。
type ProfileBundle struct {
	Version    int           `json:"version"`
	ExportedAt int64         `json:"exportedAt"`
	Conns      []ConnProfile `json:"conns"`
}

// ExportProfiles 导出连接档案（默认不含密码）；文件 0600。
func ExportProfiles(path string, conns []ConnProfile, includePassword bool) (int, error) {
	out := ProfileBundle{Version: 1, ExportedAt: time.Now().UnixMilli()}
	for _, c := range conns {
		if !includePassword {
			c.Password = ""
		}
		out.Conns = append(out.Conns, c)
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return 0, err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return 0, err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(out.Conns), nil
}

// profileKey 是导入去重的关键字段（不含密码与 id）。
func profileKey(p ConnProfile) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s", p.Kind, p.Host, p.Port, p.User, p.Database, p.Path, p.Name)
}

// ImportProfiles 读取导出文件，返回需要新增的连接（id 冲突或重复的会被跳过/重映射）。
// 去重：与 existing 关键字段完全相同的跳过；导入的 id 若与 existing 冲突则重新生成。
func ImportProfiles(path string, existing []ConnProfile) ([]ConnProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bundle ProfileBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("导入文件不是有效的连接档案: %w", err)
	}
	known := map[string]bool{}
	for _, p := range existing {
		known[profileKey(p)] = true
	}
	ids := map[string]bool{}
	for _, p := range existing {
		ids[p.ID] = true
	}
	var out []ConnProfile
	for _, p := range bundle.Conns {
		key := profileKey(p)
		if known[key] {
			continue
		}
		known[key] = true
		if p.ID == "" || ids[p.ID] {
			id, err := genID()
			if err != nil {
				return nil, err
			}
			p.ID = id
		}
		ids[p.ID] = true
		out = append(out, p)
	}
	return out, nil
}
