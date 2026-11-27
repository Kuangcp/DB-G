package app

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"db-g/locales"
)

func (s *ConnStore) stripConsoleRefs(gone map[string]bool) {
	if len(gone) == 0 {
		return
	}
	for i := range s.workspaces {
		ws := &s.workspaces[i]
		if gone[ws.LastActiveConsoleID] {
			ws.LastActiveConsoleID = ""
		}
		if len(ws.Members) == 0 {
			continue
		}
		kept := ws.Members[:0]
		for _, id := range ws.Members {
			if gone[id] {
				continue
			}
			kept = append(kept, id)
		}
		ws.Members = kept
	}
}

// Active 返回当前活动连接。
func (s *ConnStore) ConsolesDir() string {
	return filepath.Join(filepath.Dir(s.path), "consoles")
}

// ListConsoles 返回某数据源的全部控制台（副本），按 SortOrder、CreatedAt 升序。
func (s *ConnStore) ListConsoles(connID string) []ConsoleRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listConsolesLocked(connID)
}

func (s *ConnStore) listConsolesLocked(connID string) []ConsoleRecord {
	out := make([]ConsoleRecord, 0, len(s.consoles))
	for _, c := range s.consoles {
		if c.ConnectionID == connID {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

// GetConsole 按 ID 查找控制台。
func (s *ConnStore) GetConsole(id string) (ConsoleRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.consoles {
		if c.ID == id {
			return c, true
		}
	}
	return ConsoleRecord{}, false
}

// CreateConsole 在指定数据源下新建控制台元数据；空名称回退「控制台 N」。
func (s *ConnStore) CreateConsole(connID, name string) (ConsoleRecord, error) {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, p := range s.conns {
		if p.ID == connID {
			found = true
			break
		}
	}
	if !found {
		return ConsoleRecord{}, errors.New(locales.AppConnMissing(connID))
	}
	if name == "" {
		name = locales.AppConsoleDefaultName(len(s.listConsolesLocked(connID)) + 1)
	}
	id, err := genID()
	if err != nil {
		return ConsoleRecord{}, err
	}
	order := 0
	for _, c := range s.consoles {
		if c.ConnectionID == connID && c.SortOrder >= order {
			order = c.SortOrder + 1
		}
	}
	now := time.Now().UnixMilli()
	rec := ConsoleRecord{
		ID:           id,
		ConnectionID: connID,
		Name:         name,
		FilePath:     filepath.Join(s.ConsolesDir(), connID+"-"+id+".sql"),
		SortOrder:    order,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.consoles = append(s.consoles, rec)
	if err := s.save(); err != nil {
		s.consoles = s.consoles[:len(s.consoles)-1]
		return ConsoleRecord{}, err
	}
	return rec, nil
}

// CreateExternalConsole 把一个外部 .sql 绑定成控制台（FilePath 指向用户文件，不复制）。
func (s *ConnStore) CreateExternalConsole(connID, name, path string) (ConsoleRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, p := range s.conns {
		if p.ID == connID {
			found = true
			break
		}
	}
	if !found {
		return ConsoleRecord{}, errors.New(locales.AppConnMissing(connID))
	}
	id, err := genID()
	if err != nil {
		return ConsoleRecord{}, err
	}
	order := 0
	for _, c := range s.consoles {
		if c.ConnectionID == connID && c.SortOrder >= order {
			order = c.SortOrder + 1
		}
	}
	now := time.Now().UnixMilli()
	rec := ConsoleRecord{
		ID:           id,
		ConnectionID: connID,
		Name:         name,
		FilePath:     path,
		External:     true,
		SortOrder:    order,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.consoles = append(s.consoles, rec)
	if err := s.save(); err != nil {
		s.consoles = s.consoles[:len(s.consoles)-1]
		return ConsoleRecord{}, err
	}
	return rec, nil
}

// GetConsoleByFilePath 按外部文件路径查找已绑定的控制台（调用方负责规范化路径）。
func (s *ConnStore) GetConsoleByFilePath(path string) (ConsoleRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.consoles {
		if c.External && c.FilePath == path {
			return c, true
		}
	}
	return ConsoleRecord{}, false
}

// RebindConsoleFile 把外部控制台改绑到新路径（另存为），不删除旧文件。
func (s *ConnStore) RebindConsoleFile(id, path, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnConsoleMissing(id))
	}
	if name == "" {
		name = filepath.Base(path)
	}
	return s.mutateConsoleLocked(idx, func(c *ConsoleRecord) {
		c.FilePath = path
		c.External = true
		c.Name = name
		c.UpdatedAt = time.Now().UnixMilli()
	})
}

// CloseConsoleEverywhere 只把控制台移出所有工作区，保留元数据与正文（外部文件绝不删除）。
func (s *ConnStore) CloseConsoleEverywhere(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consoleIndexLocked(id) < 0 {
		return errors.New(locales.AppConnConsoleMissing(id))
	}
	prev := s.snapshot()
	s.stripConsoleRefs(map[string]bool{id: true})
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// RenameConsole 改名；空名称回退「控制台 N」之外的默认值（保留旧名）。
func (s *ConnStore) RenameConsole(id, name string) error {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnConsoleMissing(id))
	}
	if name == "" {
		name = s.consoles[idx].Name
	}
	// 用更新闭包做单点写入，save 失败时回滚。
	return s.mutateConsoleLocked(idx, func(c *ConsoleRecord) {
		c.Name = name
		c.UpdatedAt = time.Now().UnixMilli()
	})
}

// DeleteConsole 删除控制台元数据行，并从所有工作区移除它的成员关系与末次激活指针。
func (s *ConnStore) DeleteConsole(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return errors.New(locales.AppConnConsoleMissing(id))
	}
	prev := s.snapshot()
	s.consoles = append(append([]ConsoleRecord{}, s.consoles[:idx]...), s.consoles[idx+1:]...)
	s.stripConsoleRefs(map[string]bool{id: true})
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// SetConsoleCaret 记住光标/选区（rune 偏移）；落盘失败只记警告，不中断编辑。
func (s *ConnStore) SetConsoleCaret(id string, start, end int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return
	}
	if err := s.mutateConsoleLocked(idx, func(c *ConsoleRecord) {
		c.CaretStart, c.CaretEnd = start, end
	}); err != nil {
		s.warning = locales.AppConnSaveCursorFailed(err.Error())
	}
}

// TouchConsole 记录控制台最近落盘时刻。
func (s *ConnStore) TouchConsole(id string, at int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return
	}
	if err := s.mutateConsoleLocked(idx, func(c *ConsoleRecord) { c.UpdatedAt = at }); err != nil {
		s.warning = locales.AppConnSaveConsoleFailed(err.Error())
	}
}

func (s *ConnStore) consoleIndexLocked(id string) int {
	for i, c := range s.consoles {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// mutateConsoleLocked 就地修改第 idx 行并落盘，失败时恢复旧值。
func (s *ConnStore) mutateConsoleLocked(idx int, fn func(*ConsoleRecord)) error {
	old := s.consoles[idx]
	fn(&s.consoles[idx])
	if err := s.save(); err != nil {
		s.consoles[idx] = old
		return err
	}
	return nil
}

// ---------- 工作区（控制台的虚拟分组） ----------

// ListWorkspaces 返回全部工作区（副本），按 SortOrder、CreatedAt 升序。
