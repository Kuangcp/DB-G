package app

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"db-g/locales"
)

func (s *ConnStore) ListWorkspaces() []WorkspaceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WorkspaceRecord, len(s.workspaces))
	for i, ws := range s.workspaces {
		out[i] = cloneWorkspace(ws)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// GetWorkspace 按 ID 查找工作区。
func (s *ConnStore) GetWorkspace(id string) (WorkspaceRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.workspaceIndexLocked(id); i >= 0 {
		return cloneWorkspace(s.workspaces[i]), true
	}
	return WorkspaceRecord{}, false
}

// CreateWorkspace 新建工作区；空名称回退「默认」并标记 AutoNamed。
func (s *ConnStore) CreateWorkspace(name string) (WorkspaceRecord, error) {
	name = strings.TrimSpace(name)
	auto := name == ""
	if auto {
		name = locales.AppWorkspaceDefault()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := genID()
	if err != nil {
		return WorkspaceRecord{}, err
	}
	order := 0
	for _, ws := range s.workspaces {
		if ws.SortOrder >= order {
			order = ws.SortOrder + 1
		}
	}
	prev := s.snapshot()
	rec := WorkspaceRecord{
		ID:        id,
		Name:      name,
		AutoNamed: auto,
		SortOrder: order,
		CreatedAt: time.Now().UnixMilli(),
		Members:   []string{},
	}
	s.workspaces = append(s.workspaces, rec)
	if err := s.save(); err != nil {
		s.restore(prev)
		return WorkspaceRecord{}, err
	}
	return cloneWorkspace(rec), nil
}

// RenameWorkspace 改名；空名称回退「默认」并保持 AutoNamed。
func (s *ConnStore) RenameWorkspace(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = locales.AppWorkspaceDefault()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(id)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(id))
	}
	prev := s.snapshot()
	s.workspaces[i].Name = name
	s.workspaces[i].AutoNamed = false
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// DeleteWorkspace 删除工作区（控制台实体与文件保留）；删的是活动工作区时清空指针。
func (s *ConnStore) DeleteWorkspace(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(id)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(id))
	}
	prev := s.snapshot()
	s.workspaces = append(append([]WorkspaceRecord{}, s.workspaces[:i]...), s.workspaces[i+1:]...)
	if s.activeWorkspaceID == id {
		s.activeWorkspaceID = ""
	}
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// AddWorkspaceMember 把控制台追加到工作区成员末尾；已存在时幂等。
func (s *ConnStore) AddWorkspaceMember(wsID, consoleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(wsID))
	}
	if s.consoleIndexLocked(consoleID) < 0 {
		return errors.New(locales.AppConnConsoleMissing(consoleID))
	}
	for _, id := range s.workspaces[i].Members {
		if id == consoleID {
			return nil
		}
	}
	prev := s.snapshot()
	s.workspaces[i].Members = append(slices.Clone(s.workspaces[i].Members), consoleID)
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// MoveWorkspaceMember 把控制台移到工作区成员里 beforeID 之前（空 = 末尾）；
// 位置没变化时不落盘。
func (s *ConnStore) MoveWorkspaceMember(wsID, consoleID, beforeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(wsID))
	}
	if s.consoleIndexLocked(consoleID) < 0 {
		return errors.New(locales.AppConnConsoleMissing(consoleID))
	}
	if beforeID == consoleID {
		return nil
	}
	ws := s.workspaces[i]
	from := -1
	for j, id := range ws.Members {
		if id == consoleID {
			from = j
			break
		}
	}
	if from < 0 {
		return errors.New(locales.AppConnConsoleNotInWorkspace(consoleID))
	}
	members := slices.Clone(ws.Members)
	members = append(members[:from], members[from+1:]...)
	to := len(members)
	if beforeID != "" {
		to = -1
		for j, id := range members {
			if id == beforeID {
				to = j
				break
			}
		}
		if to < 0 {
			return errors.New(locales.AppConnTargetConsoleNotInWorkspace(beforeID))
		}
	}
	members = slices.Insert(members, to, consoleID)
	if slices.Equal(members, ws.Members) {
		return nil
	}
	prev := s.snapshot()
	s.workspaces[i].Members = members
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// RemoveWorkspaceMember 从工作区移出控制台；清空指向它的末次激活指针。
func (s *ConnStore) RemoveWorkspaceMember(wsID, consoleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(wsID))
	}
	prev := s.snapshot()
	ws := &s.workspaces[i]
	kept := make([]string, 0, len(ws.Members))
	for _, id := range ws.Members {
		if id != consoleID {
			kept = append(kept, id)
		}
	}
	ws.Members = kept
	if ws.LastActiveConsoleID == consoleID {
		ws.LastActiveConsoleID = ""
	}
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// SetWorkspaceLastActive 记住工作区上次激活的控制台（重启回位）。
func (s *ConnStore) SetWorkspaceLastActive(wsID, consoleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return errors.New(locales.AppConnWorkspaceMissing(wsID))
	}
	if s.workspaces[i].LastActiveConsoleID == consoleID {
		return nil
	}
	prev := s.snapshot()
	s.workspaces[i].LastActiveConsoleID = consoleID
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// ActiveWorkspaceID 返回持久化的活动工作区 id（空串 = 无）。
func (s *ConnStore) ActiveWorkspaceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeWorkspaceID
}

// SetActiveWorkspace 设置活动工作区；空串清空；落盘失败回滚并记警告。
func (s *ConnStore) SetActiveWorkspace(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.activeWorkspaceID
	s.activeWorkspaceID = id
	if err := s.save(); err != nil {
		s.activeWorkspaceID = prev
		s.warning = locales.AppConnSaveWorkspaceFailed(err.Error())
	}
}

func (s *ConnStore) workspaceIndexLocked(id string) int {
	for i, ws := range s.workspaces {
		if ws.ID == id {
			return i
		}
	}
	return -1
}

func cloneWorkspace(ws WorkspaceRecord) WorkspaceRecord {
	ws.Members = slices.Clone(ws.Members)
	return ws
}
