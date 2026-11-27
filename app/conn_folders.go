package app

import (
	"errors"
	"slices"
	"strings"

	"db-g/locales"
)

func (s *ConnStore) folderIndexLocked(id string) int {
	for i := range s.folders {
		if s.folders[i].ID == id {
			return i
		}
	}
	return -1
}

// folderNameTakenLocked 判断同层级是否已有同名文件夹（skipID 用于重命名）。
func (s *ConnStore) folderNameTakenLocked(parentID, name, skipID string) bool {
	for _, f := range s.folders {
		if f.ID != skipID && f.ParentID == parentID && f.Name == name {
			return true
		}
	}
	return false
}

// Add 校验并新增连接；ID 为空时自动生成。
func (s *ConnStore) AddFolder(name, parentID string) (ConnFolder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ConnFolder{}, errors.New(locales.AppConnFolderNameEmpty())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if parentID != "" && s.folderIndexLocked(parentID) < 0 {
		return ConnFolder{}, errors.New(locales.AppConnFolderParentMissing(parentID))
	}
	if s.folderNameTakenLocked(parentID, name, "") {
		return ConnFolder{}, errors.New(locales.AppConnFolderDuplicate())
	}
	id, err := genID()
	if err != nil {
		return ConnFolder{}, err
	}
	f := ConnFolder{ID: id, Name: name, ParentID: parentID}
	prev := s.snapshot()
	s.folders = append(s.folders, f)
	if err := s.save(); err != nil {
		s.restore(prev)
		return ConnFolder{}, err
	}
	return f, nil
}

// RenameFolder 重命名文件夹；同层级不允许重名。
func (s *ConnStore) RenameFolder(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New(locales.AppConnFolderNameEmpty())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.folderIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnFolderMissing(id))
	}
	if s.folderNameTakenLocked(s.folders[idx].ParentID, name, id) {
		return errors.New(locales.AppConnFolderDuplicate())
	}
	prev := s.snapshot()
	s.folders[idx].Name = name
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// DeleteFolder 删除文件夹：其中的连接和子文件夹都上移到它的父级。
// 连接配置、控制台与数据库本身都不删除。
func (s *ConnStore) DeleteFolder(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.folderIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnFolderMissing(id))
	}
	parent := s.folders[idx].ParentID
	prev := s.snapshot()
	for i := range s.folders {
		if i != idx && s.folders[i].ParentID == id {
			s.folders[i].ParentID = parent
		}
	}
	for i := range s.conns {
		if s.conns[i].FolderID == id {
			s.conns[i].FolderID = parent
		}
	}
	s.folders = append(s.folders[:idx], s.folders[idx+1:]...)
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// MoveConnection 把连接移到另一个文件夹（空串 = 根级），并放到该文件夹末尾。
// 连接已经在目标文件夹时不做任何事。
func (s *ConnStore) MoveConnection(id, folderID string) error {
	return s.MoveConnectionTo(id, folderID, "")
}

// MoveConnectionTo 把连接移到 folderID，插在 beforeID 之前；beforeID 为空表示
// 放到该文件夹末尾。folderID 与 beforeID 必须属于同一层级，否则拒绝。
func (s *ConnStore) MoveConnectionTo(id, folderID, beforeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if folderID != "" && s.folderIndexLocked(folderID) < 0 {
		return errors.New(locales.AppConnFolderMissing(folderID))
	}
	idx := -1
	for i := range s.conns {
		if s.conns[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New(locales.AppConnMissing(id))
	}
	if beforeID == id {
		return nil
	}
	if beforeID != "" {
		bi := -1
		for i := range s.conns {
			if s.conns[i].ID == beforeID {
				bi = i
				break
			}
		}
		if bi < 0 {
			return errors.New(locales.AppConnTargetConnMissing(beforeID))
		}
		if s.conns[bi].FolderID != folderID {
			return errors.New(locales.AppConnTargetConnNotInFolder())
		}
	}
	prev := s.snapshot()
	p := s.conns[idx]
	p.FolderID = folderID
	s.conns = append(s.conns[:idx], s.conns[idx+1:]...)
	at := len(s.conns)
	if beforeID != "" {
		for i := range s.conns {
			if s.conns[i].ID == beforeID {
				at = i
				break
			}
		}
	}
	s.conns = slices.Insert(s.conns, at, p)
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// folderDescendantLocked 判断 id 是否是 rootID 自身或它的子孙。
func (s *ConnStore) folderDescendantLocked(rootID, id string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		if id == rootID {
			return true
		}
		seen[id] = true
		idx := s.folderIndexLocked(id)
		if idx < 0 {
			return false
		}
		id = s.folders[idx].ParentID
	}
	return false
}

// MoveFolderTo 把文件夹移到 parentID，插在 beforeID 之前；beforeID 为空表示
// 放到该层级末尾。不能移进自身或自己的子孙。
func (s *ConnStore) MoveFolderTo(id, parentID, beforeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.folderIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnFolderMissing(id))
	}
	if parentID != "" && s.folderIndexLocked(parentID) < 0 {
		return errors.New(locales.AppConnFolderParentMissing(parentID))
	}
	if parentID != "" && s.folderDescendantLocked(id, parentID) {
		return errors.New(locales.AppConnFolderSelf())
	}
	if beforeID == id {
		return nil
	}
	if beforeID != "" {
		bi := s.folderIndexLocked(beforeID)
		if bi < 0 {
			return errors.New(locales.AppConnFolderTargetMissing(beforeID))
		}
		if s.folders[bi].ParentID != parentID {
			return errors.New(locales.AppConnFolderNotSibling())
		}
	}
	prev := s.snapshot()
	f := s.folders[idx]
	f.ParentID = parentID
	s.folders = append(s.folders[:idx], s.folders[idx+1:]...)
	at := len(s.folders)
	if beforeID != "" {
		at = s.folderIndexLocked(beforeID)
		if at < 0 {
			at = len(s.folders)
		}
	}
	s.folders = slices.Insert(s.folders, at, f)
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// FolderPath 返回文件夹的完整路径（如 "生产 / 订单"）；不存在或成环时返回空串。
func FolderPath(folders []ConnFolder, id string) string {
	byID := make(map[string]ConnFolder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	var parts []string
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		f, ok := byID[id]
		if !ok {
			break
		}
		seen[id] = true
		parts = append(parts, f.Name)
		id = f.ParentID
	}
	slices.Reverse(parts)
	return strings.Join(parts, " / ")
}

// stripConsoleRefs 从所有工作区移除这些控制台成员，并清空指向它们的末次激活。
