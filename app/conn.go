package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
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

// ConsoleRecord 是一个命名控制台的元数据；正文存在它绑定的 .sql 文件里。
type ConsoleRecord struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionID"`
	Name         string `json:"name"`
	FilePath     string `json:"filePath"`
	SortOrder    int    `json:"sortOrder"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	CaretStart   int    `json:"caretStart"`
	CaretEnd     int    `json:"caretEnd"`
}

// storeFile 是 connections.json 的全部内容；LoadStore 与 save 共用，避免两处漂移。
type storeFile struct {
	Conns             []ConnProfile     `json:"conns"`
	ActiveID          string            `json:"activeID"`
	Consoles          []ConsoleRecord   `json:"consoles"`
	Workspaces        []WorkspaceRecord `json:"workspaces"`
	ActiveWorkspaceID string            `json:"activeWorkspaceID"`
}

// WorkspaceRecord 是一个控制台分组：成员数组的顺序即标签顺序。
type WorkspaceRecord struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	AutoNamed           bool     `json:"autoNamed"`
	SortOrder           int      `json:"sortOrder"`
	CreatedAt           int64    `json:"createdAt"`
	LastActiveConsoleID string   `json:"lastActiveConsoleID"`
	Members             []string `json:"members"`
}

// ConnStore 管理连接列表、控制台元数据、工作区与活动指针；变更同步原子落盘。
type ConnStore struct {
	mu                sync.Mutex
	path              string
	conns             []ConnProfile
	activeID          string
	consoles          []ConsoleRecord
	workspaces        []WorkspaceRecord
	activeWorkspaceID string
	warning           string
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
	var file storeFile
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
	s.consoles = file.Consoles
	s.workspaces = file.Workspaces
	s.activeWorkspaceID = file.ActiveWorkspaceID
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
	data, err := json.MarshalIndent(storeFile{
		Conns:             s.conns,
		ActiveID:          s.activeID,
		Consoles:          s.consoles,
		Workspaces:        s.workspaces,
		ActiveWorkspaceID: s.activeWorkspaceID,
	}, "", "  ")
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

// storeState 是落盘失败时的回滚快照（切片深拷贝）。
type storeState struct {
	conns             []ConnProfile
	consoles          []ConsoleRecord
	workspaces        []WorkspaceRecord
	activeID          string
	activeWorkspaceID string
}

func (s *ConnStore) snapshot() storeState {
	return storeState{
		conns:             slices.Clone(s.conns),
		consoles:          slices.Clone(s.consoles),
		workspaces:        slices.Clone(s.workspaces),
		activeID:          s.activeID,
		activeWorkspaceID: s.activeWorkspaceID,
	}
}

func (s *ConnStore) restore(st storeState) {
	s.conns, s.consoles, s.workspaces = st.conns, st.consoles, st.workspaces
	s.activeID, s.activeWorkspaceID = st.activeID, st.activeWorkspaceID
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
	case "mysql", "postgres", "clickhouse":
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
	prev := s.snapshot()
	s.conns = append(s.conns, p)
	if err := s.save(); err != nil {
		s.restore(prev)
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
			prev := s.snapshot()
			s.conns[i] = p
			if err := s.save(); err != nil {
				s.restore(prev)
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("连接不存在: %s", p.ID)
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
	return fmt.Errorf("连接不存在: %s", id)
}

// stripConsoleRefs 从所有工作区移除这些控制台成员，并清空指向它们的末次激活。
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

// ConsolesDir 返回控制台正文文件目录（与配置文件同级，测试天然隔离）。
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
		return ConsoleRecord{}, fmt.Errorf("连接不存在: %s", connID)
	}
	if name == "" {
		name = fmt.Sprintf("控制台 %d", len(s.listConsolesLocked(connID))+1)
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
		FilePath:     filepath.Join(s.ConsolesDir(), id+".sql"),
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

// RenameConsole 改名；空名称回退「控制台 N」之外的默认值（保留旧名）。
func (s *ConnStore) RenameConsole(id, name string) error {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.consoleIndexLocked(id)
	if idx < 0 {
		return fmt.Errorf("控制台不存在: %s", id)
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
		return fmt.Errorf("控制台不存在: %s", id)
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
		s.warning = "保存控制台光标失败: " + err.Error()
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
		s.warning = "保存控制台失败: " + err.Error()
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
		name = "默认"
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
		name = "默认"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("工作区不存在: %s", id)
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
		return fmt.Errorf("工作区不存在: %s", id)
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
		return fmt.Errorf("工作区不存在: %s", wsID)
	}
	if s.consoleIndexLocked(consoleID) < 0 {
		return fmt.Errorf("控制台不存在: %s", consoleID)
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

// RemoveWorkspaceMember 从工作区移出控制台；清空指向它的末次激活指针。
func (s *ConnStore) RemoveWorkspaceMember(wsID, consoleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return fmt.Errorf("工作区不存在: %s", wsID)
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
		return fmt.Errorf("工作区不存在: %s", wsID)
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
		s.warning = "保存活动工作区失败: " + err.Error()
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
