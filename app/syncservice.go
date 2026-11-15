package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"db-g/sync"
)

// SyncOutcome 是一次拉取/提交的结果，供 UI 直接展示。
type SyncOutcome struct {
	OK         bool
	Message    string
	NeedsKey   bool
	RepoHasKCV bool
	RepoKCV    string
	Applied    bool
	Findings   []SecretFinding
	Removed    int
	Preserved  int
	Skipped    int
	Written    int
	Deleted    int
}

// SyncService 把 ConnStore 的快照与 git 仓库编排起来；单次动作串行执行由 UI 保证。
type SyncService struct {
	Store     *ConnStore
	PrefsPath string
	Hostname  string
}

// NewSyncService 创建服务。
func NewSyncService(store *ConnStore, prefsPath string) *SyncService {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "local"
	}
	return &SyncService{Store: store, PrefsPath: prefsPath, Hostname: host}
}

func prefsBranch(path string) string {
	prefs, _ := LoadSyncPrefs(path)
	return prefs.Branch
}

func (s *SyncService) engine(prefs SyncPrefs) *sync.Engine {
	return sync.NewEngine(SyncWorkTree(s.PrefsPath), prefs.RepoURL, prefs.Branch, prefs.SSHKeyPath)
}

// ProbeKey 只准备仓库并读 KCV，不触碰本地数据；用于拉取/提交前判断是否需要导入密钥。
func (s *SyncService) ProbeKey(ctx context.Context) (string, bool, error) {
	prefs, warn := LoadSyncPrefs(s.PrefsPath)
	if warn != "" {
		return "", false, errors.New(warn)
	}
	if strings.TrimSpace(prefs.RepoURL) == "" {
		return "", false, errors.New("请先在设置 → 同步中填写仓库地址")
	}
	if !sync.Available(ctx) {
		return "", false, errors.New("未找到系统 git，请先安装 git")
	}
	eng := s.engine(prefs)
	if _, err := eng.EnsureRepo(ctx); err != nil {
		return "", false, err
	}
	kcv, ok := eng.RepoKCV(ctx)
	return kcv, ok, nil
}

// SaveKey 保存本机同步密钥并更新 KCV。
func (s *SyncService) SaveKey(key []byte) error { return SaveSyncKey(s.PrefsPath, key) }

// AcceptFindings 记住已确认的敏感命中，避免同一处反复弹窗。
func (s *SyncService) AcceptFindings(ids []string) error {
	prefs, _ := LoadSyncPrefs(s.PrefsPath)
	seen := map[string]bool{}
	for _, id := range prefs.AcceptedFindings {
		seen[id] = true
	}
	for _, id := range ids {
		if id != "" && !seen[id] {
			prefs.AcceptedFindings = append(prefs.AcceptedFindings, id)
			seen[id] = true
		}
	}
	if len(prefs.AcceptedFindings) > 500 {
		prefs.AcceptedFindings = prefs.AcceptedFindings[len(prefs.AcceptedFindings)-500:]
	}
	return SaveSyncPrefs(s.PrefsPath, prefs)
}

// Run 执行 push 或 pull。
func (s *SyncService) Run(ctx context.Context, action string) SyncOutcome {
	LogInfo("sync", "run", "action", action, "branch", strings.TrimSpace(prefsBranch(s.PrefsPath)))
	prefs, warn := LoadSyncPrefs(s.PrefsPath)
	if warn != "" {
		return SyncOutcome{Message: warn}
	}
	if strings.TrimSpace(prefs.RepoURL) == "" {
		return SyncOutcome{Message: "请先在设置 → 同步中填写仓库地址"}
	}
	if !sync.Available(ctx) {
		return SyncOutcome{Message: "未找到系统 git，请先安装 git"}
	}
	eng := s.engine(prefs)
	cloned, err := eng.EnsureRepo(ctx)
	if err != nil {
		return SyncOutcome{Message: "准备同步仓库失败：" + gitMessage(err)}
	}
	key, ok := LoadSyncKey(s.PrefsPath)
	if !ok {
		kcv, has := eng.RepoKCV(ctx)
		return SyncOutcome{
			Message:    "需要同步密钥",
			NeedsKey:   true,
			RepoHasKCV: has,
			RepoKCV:    kcv,
		}
	}
	if err := eng.CheckKey(ctx, key); err != nil {
		switch {
		case errors.Is(err, sync.ErrKeyMismatch):
			return SyncOutcome{Message: "同步密钥与仓库不匹配，请导入正确的 sync.key"}
		case errors.Is(err, sync.ErrRepoKeyMissing):
			return SyncOutcome{Message: err.Error()}
		default:
			return SyncOutcome{Message: "校验同步密钥失败：" + err.Error()}
		}
	}
	if action == "pull" {
		return s.pull(ctx, eng, prefs, key, cloned)
	}
	return s.push(ctx, eng, prefs, key, cloned)
}

func (s *SyncService) push(ctx context.Context, eng *sync.Engine, prefs SyncPrefs, key []byte, cloned bool) SyncOutcome {
	snap := s.Snapshot()
	if len(snap.Conns) == 0 && eng.RemoteHasConnections(ctx) {
		return SyncOutcome{Message: "本地连接为空、远端已有数据；已阻止推送，避免清空仓库"}
	}
	if prefs.LastSyncAt == 0 && eng.RemoteHasConnections(ctx) {
		return SyncOutcome{Message: "远端已有同步数据；请先拉取导入，再提交本地改动"}
	}
	files, err := eng.BuildFiles(key, snap, sync.KCV(key))
	if err != nil {
		return SyncOutcome{Message: "生成同步文件失败：" + err.Error()}
	}
	if findings := s.scan(prefs, key, files); len(findings) > 0 {
		return SyncOutcome{
			Message:  fmt.Sprintf("发现 %d 处疑似明文凭据", len(findings)),
			Findings: findings,
		}
	}
	stats, err := eng.WriteFiles(files)
	if err != nil {
		return SyncOutcome{Message: "写入同步目录失败：" + err.Error()}
	}
	if _, err := eng.Git.Commit(ctx, fmt.Sprintf("db-g sync: %s %s", s.Hostname, time.Now().Format(time.RFC3339))); err != nil {
		return SyncOutcome{Message: "git 提交失败：" + gitMessage(err)}
	}

	// 提交本地配置后先把远端合并进来，再推送；冲突时整体中止，不 push。
	headBefore := eng.Head(ctx)
	merged := false
	if paths, err := eng.MergeRemote(ctx); err != nil {
		var ce *sync.ConflictError
		switch {
		case errors.Is(err, sync.ErrNoRemoteRef):
			// 远端还没有分支：首次 push，直接推。
		case errors.As(err, &ce) || len(paths) > 0:
			return SyncOutcome{Message: "合并远端失败（冲突），已放弃本次提交：" + strings.Join(paths, ", ")}
		default:
			return SyncOutcome{Message: "合并远端失败：" + gitMessage(err)}
		}
	} else {
		merged = eng.Head(ctx) != headBefore
	}
	removed := 0
	mergedPreserved := 0
	if merged {
		tree, skipped, err := eng.ReadTree(key)
		if err != nil {
			return SyncOutcome{Message: "读取合并结果失败：" + err.Error()}
		}
		tree, preserved := mergeSyncTrees(tree, s.Snapshot())
		gone, err := s.apply(tree)
		if err != nil {
			return SyncOutcome{Message: "导入合并结果失败：" + err.Error()}
		}
		removed = len(gone)
		mergedPreserved = preserved
		LogInfo("sync", "push merged remote", "conns", len(tree.Conns), "consoles", len(tree.Consoles), "skipped", skipped, "preserved", preserved)
	}
	if err := eng.Git.Push(ctx, prefs.Branch); err != nil {
		return SyncOutcome{Message: pushMessage(err)}
	}
	eng.BindBranch(ctx)
	s.markSynced()
	LogInfo("sync", "push ok", "written", stats.Written, "deleted", stats.Deleted, "conns", len(snap.Conns), "consoles", len(snap.Consoles), "merged", merged)
	return SyncOutcome{
		OK: true, Written: stats.Written, Deleted: stats.Deleted, Applied: merged, Removed: removed,
		Preserved: mergedPreserved,
		Message:   fmt.Sprintf("已提交并推送：写入 %d、删除 %d 个文件", stats.Written, stats.Deleted),
	}
}

func (s *SyncService) pull(ctx context.Context, eng *sync.Engine, prefs SyncPrefs, key []byte, cloned bool) SyncOutcome {
	if _, ok := eng.RemoteRef(ctx); !ok {
		// 远端还没有提交：这次拉取只完成 clone/分支绑定，不报错。
		eng.BindBranch(ctx)
		s.markSynced()
		return SyncOutcome{
			OK:      true,
			Message: fmt.Sprintf("远端仓库为空，已克隆并绑定分支 %s；可点击「提交并推送」", prefs.Branch),
		}
	}
	if eng.Dirty(ctx) {
		return SyncOutcome{Message: "同步目录有未提交改动，请先点击「提交并推送」"}
	}
	if paths, err := eng.MergeRemote(ctx); err != nil {
		var ce *sync.ConflictError
		if errors.As(err, &ce) || len(paths) > 0 {
			return SyncOutcome{Message: "合并冲突，已放弃本次拉取：" + strings.Join(paths, ", ")}
		}
		return SyncOutcome{Message: "拉取失败：" + gitMessage(err)}
	}
	eng.BindBranch(ctx)
	msg := "已拉取并导入"
	if cloned {
		msg = "已克隆仓库、检出分支并导入"
	}
	return s.importTree(eng, prefs, key, msg)
}

func (s *SyncService) importTree(eng *sync.Engine, prefs SyncPrefs, key []byte, okMsg string) SyncOutcome {
	tree, skipped, err := eng.ReadTree(key)
	if err != nil {
		return SyncOutcome{Message: "读取仓库数据失败：" + err.Error()}
	}
	mergedTree, preserved := mergeSyncTrees(tree, s.Snapshot())
	removed, err := s.apply(mergedTree)
	if err != nil {
		return SyncOutcome{Message: "导入本地失败：" + err.Error()}
	}
	s.markSynced()
	LogInfo("sync", "pull ok", "remote_conns", len(tree.Conns), "remote_consoles", len(tree.Consoles),
		"preserved", preserved, "skipped", skipped, "removed", len(removed))
	msg := fmt.Sprintf("%s：远端 %d 个连接、%d 个控制台（跳过 %d 个）",
		okMsg, len(tree.Conns), len(tree.Consoles), skipped)
	if preserved > 0 {
		msg += fmt.Sprintf("；本地独有的 %d 项已保留，点「提交并推送」可共存", preserved)
	}
	return SyncOutcome{
		OK: true, Applied: true, Removed: len(removed), Preserved: preserved, Skipped: skipped,
		Message: msg,
	}
}

// Snapshot 读取本地全量同步态（内部控制台；外部文件控制台不同步）。
func (s *SyncService) Snapshot() sync.Snapshot {
	var snap sync.Snapshot
	for _, p := range s.Store.List() {
		c := sync.Conn{
			FormatVersion: sync.FormatVersion,
			ID:            p.ID, Name: p.Name, Kind: p.Kind, Path: p.Path,
			Host: p.Host, Port: p.Port, User: p.User, Password: p.Password,
			Database: p.Database, Protocol: p.Protocol, Params: p.Params,
			FolderID: p.FolderID, QueryTimeout: p.QueryTimeout, GuardBlock: p.GuardBlock,
		}
		for _, t := range p.Tags {
			c.Tags = append(c.Tags, sync.Tag{Label: t.Label, Color: t.Color})
		}
		snap.Conns = append(snap.Conns, c)
	}
	for _, f := range s.Store.Folders() {
		snap.Folders = append(snap.Folders, sync.Folder{ID: f.ID, Name: f.Name, ParentID: f.ParentID})
	}
	for _, w := range s.Store.ListWorkspaces() {
		snap.Workspaces = append(snap.Workspaces, sync.Workspace{
			ID: w.ID, Name: w.Name, AutoNamed: w.AutoNamed, SortOrder: w.SortOrder,
			CreatedAt: w.CreatedAt, LastActiveConsoleID: w.LastActiveConsoleID,
			Members: append([]string(nil), w.Members...),
		})
	}
	for _, rec := range s.Store.AllConsoles() {
		if rec.External {
			continue
		}
		body, _ := os.ReadFile(s.consolePath(rec))
		snap.Consoles = append(snap.Consoles, sync.Console{
			Meta: sync.ConsoleMeta{
				ID: rec.ID, ConnectionID: rec.ConnectionID, Name: rec.Name,
				SortOrder: rec.SortOrder, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
			},
			Body: string(body),
		})
	}
	return snap
}

func (s *SyncService) consolePath(rec ConsoleRecord) string {
	return filepath.Join(s.Store.ConsolesDir(), rec.ConnectionID+"-"+rec.ID+".sql")
}
