package app

import (
	"path/filepath"
	"time"
)

// AllConsoles 返回全部控制台记录（副本，顺序即存储顺序）。
func (s *ConnStore) AllConsoles() []ConsoleRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ConsoleRecord(nil), s.consoles...)
}

// SyncApplyStats 是一次导入的结果统计。
type SyncApplyStats struct {
	Removed []ConsoleRecord
	Skipped int
}

// ApplySync 用合并后的快照替换本地连接库：保留外部控制台与设备本地状态，只落盘一次。
func (s *ConnStore) ApplySync(conns []ConnProfile, folders []ConnFolder, consoles []ConsoleRecord, workspaces []WorkspaceRecord) (SyncApplyStats, error) {
	var stats SyncApplyStats
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.snapshot()

	// 连接：只做轻量校验；SQLite 路径可能只在远端设备存在，不能因此拒绝导入。
	nextConns := make([]ConnProfile, 0, len(conns))
	connSet := make(map[string]bool, len(conns))
	for _, p := range conns {
		if p.ID == "" || p.Name == "" || p.Kind == "" || connSet[p.ID] {
			stats.Skipped++
			continue
		}
		p.Tags = NormalizeTags(p.Tags)
		connSet[p.ID] = true
		nextConns = append(nextConns, p)
	}

	nextFolders := make([]ConnFolder, 0, len(folders))
	folderSet := map[string]bool{}
	for _, f := range folders {
		if f.ID == "" || folderSet[f.ID] {
			stats.Skipped++
			continue
		}
		folderSet[f.ID] = true
		nextFolders = append(nextFolders, f)
	}
	folderSet = map[string]bool{}
	for _, f := range nextFolders {
		folderSet[f.ID] = true
	}
	keptFolders := nextFolders[:0]
	for _, f := range nextFolders {
		if f.ParentID != "" && !folderSet[f.ParentID] {
			stats.Skipped++
			f.ParentID = ""
		}
		keptFolders = append(keptFolders, f)
	}
	nextFolders = keptFolders
	for i := range nextConns {
		if nextConns[i].FolderID != "" && !folderSet[nextConns[i].FolderID] {
			nextConns[i].FolderID = ""
		}
	}

	// 控制台：外部控制台与设备本地光标保留；引用不存在连接的跳过。
	existing := make(map[string]ConsoleRecord, len(s.consoles))
	for _, c := range s.consoles {
		existing[c.ID] = c
	}
	var external []ConsoleRecord
	for _, c := range s.consoles {
		if c.External {
			external = append(external, c)
		}
	}
	nextConsoles := make([]ConsoleRecord, 0, len(consoles)+len(external))
	consoleSet := map[string]bool{}
	for _, c := range consoles {
		if c.ID == "" || c.External || !connSet[c.ConnectionID] || consoleSet[c.ID] {
			stats.Skipped++
			continue
		}
		if old, ok := existing[c.ID]; ok && old.External {
			// 同一 id 被本地外部控制台占用：保留用户文件绑定，不覆盖。
			stats.Skipped++
			continue
		}
		c.External = false
		c.FilePath = filepath.Join(s.ConsolesDir(), c.ConnectionID+"-"+c.ID+".sql")
		if old, ok := existing[c.ID]; ok && !old.External {
			c.CaretStart, c.CaretEnd = old.CaretStart, old.CaretEnd
		}
		consoleSet[c.ID] = true
		nextConsoles = append(nextConsoles, c)
	}
	for _, old := range s.consoles {
		if old.External || consoleSet[old.ID] {
			continue
		}
		stats.Removed = append(stats.Removed, old)
	}
	nextConsoles = append(nextConsoles, external...)

	// 工作区：成员只保留仍存在的控制台；本机外部控制台的成员关系不因拉取丢失。
	allConsoles := map[string]bool{}
	externalIDs := map[string]bool{}
	for _, c := range nextConsoles {
		allConsoles[c.ID] = true
		if c.External {
			externalIDs[c.ID] = true
		}
	}
	oldWorkspaces := map[string]WorkspaceRecord{}
	for _, w := range s.workspaces {
		oldWorkspaces[w.ID] = w
	}
	nextWorkspaces := make([]WorkspaceRecord, 0, len(workspaces))
	wsSet := map[string]bool{}
	for _, w := range workspaces {
		if w.ID == "" || wsSet[w.ID] {
			stats.Skipped++
			continue
		}
		wsSet[w.ID] = true
		members := w.Members[:0]
		for _, id := range w.Members {
			if allConsoles[id] {
				members = append(members, id)
			}
		}
		if old, ok := oldWorkspaces[w.ID]; ok {
			seen := map[string]bool{}
			for _, id := range members {
				seen[id] = true
			}
			for _, id := range old.Members {
				if externalIDs[id] && !seen[id] {
					members = append(members, id)
					seen[id] = true
				}
			}
		}
		w.Members = members
		if w.LastActiveConsoleID != "" && !allConsoles[w.LastActiveConsoleID] {
			w.LastActiveConsoleID = ""
		}
		nextWorkspaces = append(nextWorkspaces, w)
	}

	s.conns, s.folders, s.consoles, s.workspaces = nextConns, nextFolders, nextConsoles, nextWorkspaces
	if !connSet[s.activeID] {
		s.activeID = ""
	}
	if !wsSet[s.activeWorkspaceID] {
		s.activeWorkspaceID = ""
	}
	if s.activeWorkspaceID == "" && len(nextWorkspaces) > 0 {
		s.activeWorkspaceID = nextWorkspaces[0].ID
	}
	if err := s.save(); err != nil {
		s.restore(prev)
		return stats, err
	}
	return stats, nil
}

// ResetForSync 丢弃控制台内存缓存与激活指针；同步导入后按 Store 重新加载。
// 不写盘、不 flush：调用方已经在导入前把编辑器内容落盘。
func (m *ConsoleManager) ResetForSync() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.saveTimers {
		if t != nil {
			t.Stop()
		}
	}
	for _, t := range m.caretTimer {
		if t != nil {
			t.Stop()
		}
	}
	m.activeWorkspaceID = ""
	m.activeConsoleID = ""
	m.cache = map[string]string{}
	m.lastSaved = map[string]string{}
	m.dirty = map[string]bool{}
	m.saveTimers = map[string]*time.Timer{}
	m.caretDraft = map[string][2]int{}
	m.caretTimer = map[string]*time.Timer{}
	m.src = map[string]func() string{}
	m.ver = map[string]uint64{}
	m.paused = map[string]bool{}
	m.writing = map[string]bool{}
}
