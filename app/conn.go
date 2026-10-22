package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ConnProfile 是一个持久化的数据源连接配置。
type ConnProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
}

// ConnStore 管理连接列表与活动连接；变更同步原子落盘。
type ConnStore struct {
	mu       sync.Mutex
	path     string
	conns    []ConnProfile
	activeID string
	warning  string
}

// DefaultStorePath 返回用户配置目录下的连接库路径。
func DefaultStorePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "connections.json"
	}
	return filepath.Join(dir, "db-g", "connections.json")
}

// LoadStore 读取持久化文件；缺失为空表，损坏改名为 .bak 后空表并记警告（改名失败则警告仅记损坏）。
func LoadStore(path string) *ConnStore {
	s := &ConnStore{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.warning = "读取连接配置失败: " + err.Error()
		}
		return s
	}
	var file struct {
		Conns    []ConnProfile `json:"conns"`
		ActiveID string        `json:"activeID"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		if rerr := os.Rename(path, path+".bak"); rerr != nil {
			s.warning = "连接配置损坏: " + rerr.Error()
		} else {
			s.warning = "连接配置损坏，已改名为 .bak"
		}
		return s
	}
	s.conns = file.Conns
	s.activeID = file.ActiveID
	return s
}

// Warning 返回最近记录的警告（加载损坏/读失败、活动连接保存失败），空串表示正常。
func (s *ConnStore) Warning() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.warning
}

// List 返回全部连接（副本）。
func (s *ConnStore) List() []ConnProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnProfile, len(s.conns))
	copy(out, s.conns)
	return out
}

// Get 按 ID 查找连接。
func (s *ConnStore) Get(id string) (ConnProfile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.conns {
		if p.ID == id {
			return p, true
		}
	}
	return ConnProfile{}, false
}

func (s *ConnStore) save() error {
	data, err := json.MarshalIndent(struct {
		Conns    []ConnProfile `json:"conns"`
		ActiveID string        `json:"activeID"`
	}{s.conns, s.activeID}, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func genID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validateProfile(p ConnProfile) error {
	if p.Name == "" {
		return errors.New("连接名称不能为空")
	}
	switch p.Kind {
	case "sqlite":
		fi, err := os.Stat(p.Path)
		if err != nil {
			return fmt.Errorf("路径不可用: %w", err)
		}
		if fi.IsDir() {
			return errors.New("路径不可用: 是目录而非数据库文件")
		}
	case "mysql":
		if p.Host == "" {
			return errors.New("主机不能为空")
		}
		if p.Port < 1 || p.Port > 65535 {
			return errors.New("端口必须在 1–65535")
		}
	default:
		return fmt.Errorf("不支持的数据源类型: %q", p.Kind)
	}
	return nil
}

// Add 校验并新增连接；ID 为空时自动生成。
func (s *ConnStore) Add(p ConnProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		id, err := genID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	for _, e := range s.conns {
		if e.ID == p.ID {
			return fmt.Errorf("连接 ID 已存在: %s", p.ID)
		}
	}
	s.conns = append(s.conns, p)
	if err := s.save(); err != nil {
		s.conns = s.conns[:len(s.conns)-1]
		return err
	}
	return nil
}

// Update 按 ID 更新连接（校验同 Add）。
func (s *ConnStore) Update(p ConnProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.ID == p.ID {
			s.conns[i] = p
			if err := s.save(); err != nil {
				s.conns[i] = e
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("连接不存在: %s", p.ID)
}

// Remove 按 ID 删除；删除活动连接时清空活动指针。
func (s *ConnStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.ID == id {
			oldActive := s.activeID
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			if s.activeID == id {
				s.activeID = ""
			}
			if err := s.save(); err != nil {
				s.conns = append(s.conns[:i], append([]ConnProfile{e}, s.conns[i:]...)...)
				s.activeID = oldActive
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("连接不存在: %s", id)
}

// Active 返回当前活动连接。
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
		s.warning = "保存活动连接失败: " + err.Error()
	}
}
