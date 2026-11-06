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
	// Protocol 只有 ClickHouse 用：native（默认）/ http / https，决定 DSN 的 scheme。
	Protocol string `json:"protocol,omitempty"`
	// Params 是额外的连接参数，key=value，如 compress=0&secure=true。
	Params string `json:"params,omitempty"`
	// FolderID 是连接所属文件夹；空串表示根级。
	FolderID string `json:"folderID,omitempty"`
	// QueryTimeout 是单条语句的查询超时（秒）；0 = 默认 30 秒。
	QueryTimeout int `json:"queryTimeout,omitempty"`
}

// ConnFolder 是侧栏连接列表里的一个文件夹；ParentID 为空表示根级。
// 同层级的显示顺序就是 Folders() 里的切片顺序，不单独存排序字段。
type ConnFolder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentID,omitempty"`
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
	Folders           []ConnFolder      `json:"folders,omitempty"`
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
	vault             *Vault
	conns             []ConnProfile
	folders           []ConnFolder
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
	// 先准备密钥（缺失时生成）：即使配置文件还不存在，后续保存也要能加密。
	if vault, verr := LoadVault(path); verr != nil {
		s.warning = "密钥不可用，密码将无法保存: " + verr.Error()
	} else {
		s.vault = vault
	}
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
	s.folders = file.Folders
	s.activeID = file.ActiveID
	s.consoles = file.Consoles
	s.workspaces = file.Workspaces
	s.activeWorkspaceID = file.ActiveWorkspaceID
	s.normalizeFoldersLocked()

	// 密码解密 + 存量明文迁移。
	vault := s.vault
	if vault == nil {
		return s
	}
	migrate := false
	for i := range s.conns {
		orig := s.conns[i].Password
		if orig == "" {
			continue
		}
		plain, derr := vault.Decrypt(orig)
		if derr != nil {
			s.conns[i].Password = ""
			s.warning = "无法解密连接密码（密钥已变更？请重新填写）: " + s.conns[i].Name
			continue
		}
		s.conns[i].Password = plain
		if !strings.HasPrefix(orig, "enc:v1:") {
			migrate = true
		}
	}
	if migrate {
		if err := s.save(); err != nil {
			s.warning = "密码自动加密回写失败: " + err.Error()
		}
	}
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

// Folders 返回全部文件夹（副本，切片顺序即同层显示顺序）。
func (s *ConnStore) Folders() []ConnFolder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConnFolder, len(s.folders))
	copy(out, s.folders)
	return out
}

// normalizeFoldersLocked 修正损坏或手改过的文件夹引用：未知父级、父子环、
// 连接指向不存在的文件夹，一律回到根级。日志记一条，便于排查。
func (s *ConnStore) normalizeFoldersLocked() {
	byID := make(map[string]int, len(s.folders))
	for i := range s.folders {
		byID[s.folders[i].ID] = i
	}
	changed := false
	for i := range s.folders {
		parent := s.folders[i].ParentID
		seen := map[string]bool{s.folders[i].ID: true}
		for parent != "" {
			idx, ok := byID[parent]
			if !ok || seen[parent] {
				s.folders[i].ParentID = ""
				changed = true
				break
			}
			seen[parent] = true
			parent = s.folders[idx].ParentID
		}
	}
	for i := range s.conns {
		if s.conns[i].FolderID == "" {
			continue
		}
		if _, ok := byID[s.conns[i].FolderID]; !ok {
			s.conns[i].FolderID = ""
			changed = true
		}
	}
	if changed {
		LogWarn("conn", "fixed orphan folder references",
			"folders", len(s.folders), "conns", len(s.conns))
	}
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
	// 落盘前加密密码；内存里始终是明文。
	conns := make([]ConnProfile, len(s.conns))
	copy(conns, s.conns)
	for i := range conns {
		if conns[i].Password == "" {
			continue
		}
		if s.vault == nil {
			return errors.New("密钥不可用，拒绝以明文写入密码")
		}
		enc, err := s.vault.Encrypt(conns[i].Password)
		if err != nil {
			return err
		}
		conns[i].Password = enc
	}
	data, err := json.MarshalIndent(storeFile{
		Conns:             conns,
		Folders:           s.folders,
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
	folders           []ConnFolder
	consoles          []ConsoleRecord
	workspaces        []WorkspaceRecord
	activeID          string
	activeWorkspaceID string
}

func (s *ConnStore) snapshot() storeState {
	return storeState{
		conns:             slices.Clone(s.conns),
		folders:           slices.Clone(s.folders),
		consoles:          slices.Clone(s.consoles),
		workspaces:        slices.Clone(s.workspaces),
		activeID:          s.activeID,
		activeWorkspaceID: s.activeWorkspaceID,
	}
}

func (s *ConnStore) restore(st storeState) {
	s.conns, s.consoles, s.workspaces = st.conns, st.consoles, st.workspaces
	s.folders = st.folders
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
	if p.QueryTimeout < 0 || p.QueryTimeout > 86400 {
		return errors.New("查询超时需在 0–86400 秒")
	}
	return nil
}

// folderIndexLocked 返回文件夹下标；不存在为 -1。调用方需持有锁。
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
func (s *ConnStore) Add(p ConnProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.FolderID != "" && s.folderIndexLocked(p.FolderID) < 0 {
		return fmt.Errorf("文件夹不存在: %s", p.FolderID)
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

// Update 按 ID 更新连接（校验同 Add）；换了文件夹时移到该文件夹末尾。
func (s *ConnStore) Update(p ConnProfile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.FolderID != "" && s.folderIndexLocked(p.FolderID) < 0 {
		return fmt.Errorf("文件夹不存在: %s", p.FolderID)
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

// AddFolder 新建文件夹；parentID 为空表示根级，同层级不允许重名。
func (s *ConnStore) AddFolder(name, parentID string) (ConnFolder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ConnFolder{}, errors.New("文件夹名称不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if parentID != "" && s.folderIndexLocked(parentID) < 0 {
		return ConnFolder{}, fmt.Errorf("父文件夹不存在: %s", parentID)
	}
	if s.folderNameTakenLocked(parentID, name, "") {
		return ConnFolder{}, errors.New("同层级已有同名文件夹")
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
		return errors.New("文件夹名称不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.folderIndexLocked(id)
	if idx < 0 {
		return fmt.Errorf("文件夹不存在: %s", id)
	}
	if s.folderNameTakenLocked(s.folders[idx].ParentID, name, id) {
		return errors.New("同层级已有同名文件夹")
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
		return fmt.Errorf("文件夹不存在: %s", id)
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
		return fmt.Errorf("文件夹不存在: %s", folderID)
	}
	idx := -1
	for i := range s.conns {
		if s.conns[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("连接不存在: %s", id)
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
			return fmt.Errorf("目标连接不存在: %s", beforeID)
		}
		if s.conns[bi].FolderID != folderID {
			return errors.New("目标连接不在该文件夹")
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
		return fmt.Errorf("文件夹不存在: %s", id)
	}
	if parentID != "" && s.folderIndexLocked(parentID) < 0 {
		return fmt.Errorf("父文件夹不存在: %s", parentID)
	}
	if parentID != "" && s.folderDescendantLocked(id, parentID) {
		return errors.New("不能把文件夹移进自身或其子文件夹")
	}
	if beforeID == id {
		return nil
	}
	if beforeID != "" {
		bi := s.folderIndexLocked(beforeID)
		if bi < 0 {
			return fmt.Errorf("目标文件夹不存在: %s", beforeID)
		}
		if s.folders[bi].ParentID != parentID {
			return errors.New("目标文件夹不在同一层级")
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

// MoveWorkspaceMember 把控制台移到工作区成员里 beforeID 之前（空 = 末尾）；
// 位置没变化时不落盘。
func (s *ConnStore) MoveWorkspaceMember(wsID, consoleID, beforeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.workspaceIndexLocked(wsID)
	if i < 0 {
		return fmt.Errorf("工作区不存在: %s", wsID)
	}
	if s.consoleIndexLocked(consoleID) < 0 {
		return fmt.Errorf("控制台不存在: %s", consoleID)
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
		return fmt.Errorf("控制台不在工作区: %s", consoleID)
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
			return fmt.Errorf("目标控制台不在工作区: %s", beforeID)
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
