package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"db-g/locales"
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
	// GuardBlock 为 true 时，高危 SQL 规则命中直接阻断执行；默认只提醒。
	GuardBlock bool `json:"guardBlock,omitempty"`
	// AIContextStrict 为 true 时，该连接只发送 schema 与当前 SQL：
	// 即使全局 AI 设置允许，也不发送 DDL、样本、历史与错误日志。
	AIContextStrict bool `json:"aiContextStrict,omitempty"`
	// Tags 是数据源标签（最多 MaxConnTags 个），用于结果区底栏标记。
	Tags []ConnTag `json:"tags,omitempty"`
}

// MaxConnTags 是一个数据源最多能拥有的标签数。
const MaxConnTags = 3

// DefaultConnTagColor 是未指定颜色的标签使用的调色板键。
const DefaultConnTagColor = "blue"

// ConnTag 是数据源上的一个彩色标签；Color 是 UI 调色板键（见 ui/tags.go）。
type ConnTag struct {
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// NormalizeTags 去掉空标签、修剪首尾空白、补默认颜色，并截断到 MaxConnTags。
// 导入/落盘路径都走它，避免旧档案里的脏标签让保存失败。
func NormalizeTags(tags []ConnTag) []ConnTag {
	out := make([]ConnTag, 0, len(tags))
	for _, tag := range tags {
		label := strings.TrimSpace(tag.Label)
		if label == "" {
			continue
		}
		color := strings.TrimSpace(tag.Color)
		if color == "" {
			color = DefaultConnTagColor
		}
		out = append(out, ConnTag{Label: label, Color: color})
		if len(out) == MaxConnTags {
			break
		}
	}
	return out
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
	External     bool   `json:"external,omitempty"` // 绑定用户磁盘上的 .sql（FilePath 是外部绝对路径）
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

// DefaultStorePath 返回用户配置目录下的连接库路径（release: db-g，开发: db-g.dev）。
func DefaultStorePath() string {
	return filepath.Join(AppConfigDir(), "connections.json")
}

// LoadStore 读取持久化文件；缺失为空表，损坏改名为 .bak 后空表并记警告（改名失败则警告仅记损坏）。
func LoadStore(path string) *ConnStore {
	s := &ConnStore{path: path}
	// 先准备密钥（缺失时生成）：即使配置文件还不存在，后续保存也要能加密。
	if vault, verr := LoadVault(path); verr != nil {
		s.warning = locales.AppConnWarnKeyUnavailable(verr.Error())
	} else {
		s.vault = vault
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.warning = locales.AppConnReadFailed(err.Error())
		}
		return s
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		if rerr := os.Rename(path, path+".bak"); rerr != nil {
			s.warning = locales.AppConnCorrupt(rerr.Error())
		} else {
			s.warning = locales.AppConnCorruptRenamed()
		}
		return s
	}
	s.conns = file.Conns
	for i := range s.conns {
		s.conns[i].Tags = NormalizeTags(s.conns[i].Tags)
	}
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
			s.warning = locales.AppConnPasswordUndecryptable(s.conns[i].Name)
			continue
		}
		s.conns[i].Password = plain
		if !strings.HasPrefix(orig, "enc:v1:") {
			migrate = true
		}
	}
	if migrate {
		if err := s.save(); err != nil {
			s.warning = locales.AppConnRewrapFailed(err.Error())
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
			return errors.New(locales.AppConnRefusePlaintext())
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
