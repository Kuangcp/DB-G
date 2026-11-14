package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"db-g/sync"
)

func (s *SyncService) scan(prefs SyncPrefs, key []byte, files map[string][]byte) []SecretFinding {
	accepted := make(map[string]bool, len(prefs.AcceptedFindings))
	for _, id := range prefs.AcceptedFindings {
		accepted[id] = true
	}
	return ScanSecrets(key, files, accepted)
}

// apply 把合并结果写入本地：先写正文，再单次替换 store，最后清理被删控制台文件。
func (s *SyncService) apply(tree sync.Snapshot) ([]ConsoleRecord, error) {
	connSet := map[string]bool{}
	for _, c := range tree.Conns {
		connSet[c.ID] = true
	}
	var consoles []ConsoleRecord
	for _, tc := range tree.Consoles {
		if !connSet[tc.Meta.ConnectionID] {
			continue
		}
		body := tc.Body
		path := filepath.Join(s.Store.ConsolesDir(), tc.Meta.ConnectionID+"-"+tc.Meta.ID+".sql")
		if err := writeSyncFile(path, []byte(body)); err != nil {
			return nil, fmt.Errorf("写控制台 %s 失败: %w", tc.Meta.ID, err)
		}
		rec := ConsoleRecord{
			ID: tc.Meta.ID, ConnectionID: tc.Meta.ConnectionID, Name: tc.Meta.Name,
			SortOrder: tc.Meta.SortOrder, CreatedAt: tc.Meta.CreatedAt, UpdatedAt: tc.Meta.UpdatedAt,
		}
		if rec.CreatedAt == 0 {
			rec.CreatedAt = time.Now().UnixMilli()
		}
		if rec.UpdatedAt == 0 {
			rec.UpdatedAt = rec.CreatedAt
		}
		consoles = append(consoles, rec)
	}
	var conns []ConnProfile
	for _, c := range tree.Conns {
		p := ConnProfile{
			ID: c.ID, Name: c.Name, Kind: c.Kind, Path: c.Path,
			Host: c.Host, Port: c.Port, User: c.User, Password: c.Password,
			Database: c.Database, Protocol: c.Protocol, Params: c.Params,
			FolderID: c.FolderID, QueryTimeout: c.QueryTimeout, GuardBlock: c.GuardBlock,
		}
		for _, t := range c.Tags {
			p.Tags = append(p.Tags, ConnTag{Label: t.Label, Color: t.Color})
		}
		conns = append(conns, p)
	}
	var folders []ConnFolder
	for _, f := range tree.Folders {
		folders = append(folders, ConnFolder{ID: f.ID, Name: f.Name, ParentID: f.ParentID})
	}
	var workspaces []WorkspaceRecord
	for _, w := range tree.Workspaces {
		workspaces = append(workspaces, WorkspaceRecord{
			ID: w.ID, Name: w.Name, AutoNamed: w.AutoNamed, SortOrder: w.SortOrder,
			CreatedAt: w.CreatedAt, LastActiveConsoleID: w.LastActiveConsoleID,
			Members: append([]string(nil), w.Members...),
		})
	}
	stats, err := s.Store.ApplySync(conns, folders, consoles, workspaces)
	if err != nil {
		return nil, err
	}
	for _, rec := range stats.Removed {
		if rec.External || rec.FilePath == "" {
			continue
		}
		_ = os.Remove(rec.FilePath)
	}
	return stats.Removed, nil
}

func writeSyncFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".db-g-sync-console-*.tmp")
	if err != nil {
		return os.WriteFile(path, data, 0o600)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}

func (s *SyncService) markSynced() {
	prefs, _ := LoadSyncPrefs(s.PrefsPath)
	prefs.LastSyncAt = time.Now().UnixMilli()
	_ = SaveSyncPrefs(s.PrefsPath, prefs)
}

var urlCredRe = regexp.MustCompile(`://[^/\s@]+@`)

// gitMessage 把 git 错误翻成可操作的中文；顺手脱敏 URL 里的凭据。
func gitMessage(err error) string {
	if err == nil {
		return ""
	}
	text := urlCredRe.ReplaceAllString(err.Error(), "://***@")
	lower := strings.ToLower(text)
	var hint string
	switch {
	case strings.Contains(lower, "permission denied (publickey)"),
		strings.Contains(lower, "could not read from remote repository"):
		hint = "SSH 认证失败：请检查私钥路径、ssh-agent 或仓库权限。"
	case strings.Contains(lower, "host key verification failed"):
		hint = "主机密钥未信任：请先用 ssh 手动连接一次并确认 host key。"
	case strings.Contains(lower, "could not read username"),
		strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "invalid username or password"):
		hint = "HTTPS 认证失败：请配置系统 git credential helper。"
	}
	if hint != "" {
		return hint + " " + truncateText(text, 300)
	}
	return truncateText(text, 400)
}

func pushMessage(err error) string {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "non-fast-forward") || strings.Contains(text, "fetch first") ||
		strings.Contains(text, "rejected") || strings.Contains(text, "updates were rejected") {
		return "远端有新提交，请先拉取后再提交"
	}
	return "推送失败：" + gitMessage(err)
}

func truncateText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
