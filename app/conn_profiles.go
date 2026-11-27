package app

import (
	"errors"
	"os"

	"db-g/locales"
)

func validateProfile(p ConnProfile) error {
	if p.Name == "" {
		return errors.New(locales.AppConnNameEmpty())
	}
	switch p.Kind {
	case "sqlite":
		fi, err := os.Stat(p.Path)
		if err != nil {
			return errors.New(locales.AppConnPathUnavailable(err.Error()))
		}
		if fi.IsDir() {
			return errors.New(locales.AppConnPathIsDir())
		}
	case "mysql", "postgres", "clickhouse":
		if p.Host == "" {
			return errors.New(locales.AppConnHostEmpty())
		}
		if p.Port < 1 || p.Port > 65535 {
			return errors.New(locales.AppConnPortRange())
		}
	default:
		return errors.New(locales.AppConnUnsupportedKind(p.Kind))
	}
	if p.QueryTimeout < 0 || p.QueryTimeout > 86400 {
		return errors.New(locales.AppConnTimeoutRange())
	}
	return nil
}

// folderIndexLocked 返回文件夹下标；不存在为 -1。调用方需持有锁。
func (s *ConnStore) Add(p ConnProfile) error {
	p.Tags = NormalizeTags(p.Tags)
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.FolderID != "" && s.folderIndexLocked(p.FolderID) < 0 {
		return errors.New(locales.AppConnFolderMissing(p.FolderID))
	}
	if p.ID == "" {
		id, err := genID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	for _, e := range s.conns {
		if e.ID == p.ID {
			return errors.New(locales.AppConnIDExists(p.ID))
		}
	}
	prev := s.snapshot()
	s.conns = append(s.conns, p)
	if err := s.save(); err != nil {
		s.restore(prev)
		return err
	}
	return nil
}

// Update 按 ID 更新连接（校验同 Add）；换了文件夹时移到该文件夹末尾。
func (s *ConnStore) Update(p ConnProfile) error {
	p.Tags = NormalizeTags(p.Tags)
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.FolderID != "" && s.folderIndexLocked(p.FolderID) < 0 {
		return errors.New(locales.AppConnFolderMissing(p.FolderID))
	}
	for i, e := range s.conns {
		if e.ID == p.ID {
			prev := s.snapshot()
			if e.FolderID != p.FolderID {
				s.conns = append(s.conns[:i], s.conns[i+1:]...)
				s.conns = append(s.conns, p)
			} else {
				s.conns[i] = p
			}
			if err := s.save(); err != nil {
				s.restore(prev)
				return err
			}
			return nil
		}
	}
	return errors.New(locales.AppConnMissing(p.ID))
}

// Remove 按 ID 删除连接，并级联删除它的控制台（含工作区成员与末次激活指针）；
// 删除活动连接时清空活动指针。
func (s *ConnStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.ID != id {
			continue
		}
		prev := s.snapshot()
		s.conns = append(s.conns[:i], s.conns[i+1:]...)
		if s.activeID == id {
			s.activeID = ""
		}
		removed := map[string]bool{}
		kept := s.consoles[:0]
		for _, c := range s.consoles {
			if c.ConnectionID == id {
				removed[c.ID] = true
				continue
			}
			kept = append(kept, c)
		}
		s.consoles = kept
		s.stripConsoleRefs(removed)
		if err := s.save(); err != nil {
			s.restore(prev)
			return err
		}
		return nil
	}
	return errors.New(locales.AppConnMissing(id))
}

// AddFolder 新建文件夹；parentID 为空表示根级，同层级不允许重名。
func (s *ConnStore) Active() (ConnProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.conns {
		if p.ID == s.activeID {
			return p, true
		}
	}
	return ConnProfile{}, false
}

// SetActive 设置活动连接；空串清空；落盘失败时回滚活动指针并记警告。
func (s *ConnStore) SetActive(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.activeID
	s.activeID = id
	if err := s.save(); err != nil {
		s.activeID = prev
		s.warning = locales.AppConnSaveActiveFailed(err.Error())
	}
}

// ConsolesDir 返回控制台正文文件目录（与配置文件同级，测试天然隔离）。
