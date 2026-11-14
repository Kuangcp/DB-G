package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrKeyMismatch 表示本地同步密钥与仓库 KCV 不一致。
var ErrKeyMismatch = errors.New("同步密钥与仓库不匹配")

// ErrRepoKeyMissing 表示仓库已有连接密文但缺少 KCV。
var ErrRepoKeyMissing = errors.New("仓库缺少 db-g/key.kcv，无法验证同步密钥")

// Engine 负责仓库工作树与 git 的搬运；不认识 app 的存储。
type Engine struct {
	WorkTree string
	Remote   string
	Branch   string
	SSHKey   string
	Git      *Git
}

// NewEngine 创建引擎。
func NewEngine(workTree, remote, branch, sshKey string) *Engine {
	if branch == "" {
		branch = "main"
	}
	return &Engine{
		WorkTree: workTree, Remote: remote, Branch: branch, SSHKey: sshKey,
		Git: NewGit(workTree, sshKey),
	}
}

// EnsureRepo 创建工作树、初始化 git、设置 origin 与本地身份，并把本地分支绑到配置分支。
func (e *Engine) EnsureRepo(ctx context.Context) error {
	if err := e.Git.Init(ctx); err != nil {
		return err
	}
	e.Git.SetIdentity(ctx)
	if err := e.Git.SetRemote(ctx, e.Remote); err != nil {
		return err
	}
	e.BindBranch(ctx)
	return nil
}

// BindBranch 让未提交的仓库使用配置的分支名，并写入 origin 跟踪配置。
// 远端还没有该分支时也先写配置，首次 push 后即可正常跟踪。
func (e *Engine) BindBranch(ctx context.Context) {
	if cur, err := e.Git.CurrentBranch(ctx); err == nil && cur != "" && cur != e.Branch && !e.Git.HasCommits(ctx) {
		_ = e.Git.SymbolicRef(ctx, "refs/heads/"+e.Branch)
	}
	e.Git.SetBranchTracking(ctx, e.Branch, "origin")
}

// Fetch 拉取远端分支。
func (e *Engine) Fetch(ctx context.Context) error { return e.Git.Fetch(ctx, e.Branch) }

// HasLocalCommits 报告工作树是否已有提交。
func (e *Engine) HasLocalCommits(ctx context.Context) bool { return e.Git.HasCommits(ctx) }

// RemoteRef 返回远端 ref。
func (e *Engine) RemoteRef(ctx context.Context) (string, bool) {
	return e.Git.RemoteRef(ctx, e.Branch)
}

// ResetToRemote 把工作树重置成远端内容（首次拉取且本地无提交）。
func (e *Engine) ResetToRemote(ctx context.Context) error {
	ref, ok := e.Git.RemoteRef(ctx, e.Branch)
	if !ok {
		return ErrNoRemoteRef
	}
	return e.Git.ResetHard(ctx, ref)
}

// RemoteHasConnections 报告远端分支是否已有 db-g/ 连接密文。
func (e *Engine) RemoteHasConnections(ctx context.Context) bool {
	ref, ok := e.Git.RemoteRef(ctx, e.Branch)
	if !ok {
		return false
	}
	return e.Git.RemoteHasEntity(ctx, ref, AppDir+"/connections")
}

// RepoKCV 返回仓库 KCV：远端分支优先（换钥后远端才是权威），其次工作树。
func (e *Engine) RepoKCV(ctx context.Context) (string, bool) {
	if ref, ok := e.Git.RemoteRef(ctx, e.Branch); ok {
		if data, ok := e.Git.ShowFile(ctx, ref, KCVFile); ok {
			if kcv := strings.TrimSpace(string(data)); kcv != "" {
				return strings.ToUpper(kcv), true
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(e.WorkTree, filepath.FromSlash(KCVFile))); err == nil {
		if kcv := strings.TrimSpace(string(data)); kcv != "" {
			return strings.ToUpper(kcv), true
		}
	}
	return "", false
}

// CheckKey 校验本地密钥与仓库 KCV；仓库没有 KCV 但已有密文时拒绝。
func (e *Engine) CheckKey(ctx context.Context, key []byte) error {
	repoKCV, has := e.RepoKCV(ctx)
	if has {
		if !KCVEqual(KCV(key), repoKCV) {
			return ErrKeyMismatch
		}
		return nil
	}
	hasEnc := false
	for _, dir := range []string{"connections", "consoles"} {
		entries, err := os.ReadDir(filepath.Join(e.WorkTree, filepath.FromSlash(AppDir), dir))
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if strings.HasSuffix(ent.Name(), ".enc") {
				hasEnc = true
				break
			}
		}
	}
	if e.RemoteHasConnections(ctx) {
		hasEnc = true
	}
	if hasEnc {
		return ErrRepoKeyMissing
	}
	return nil
}

// MergeRemote 合并 origin/<branch>；冲突时中止并返回路径。
func (e *Engine) MergeRemote(ctx context.Context) ([]string, error) {
	ref, ok := e.Git.RemoteRef(ctx, e.Branch)
	if !ok {
		return nil, ErrNoRemoteRef
	}
	allowUnrelated := !e.Git.HasMergeBase(ctx, "HEAD", ref)
	if err := e.Git.Merge(ctx, ref, allowUnrelated); err != nil {
		paths := e.Git.Conflicted(ctx)
		e.Git.MergeAbort(ctx)
		if len(paths) > 0 {
			return paths, &ConflictError{Paths: paths, Err: err}
		}
		return nil, err
	}
	return nil, nil
}

// BuildFiles 生成仓库文件集合（尚未写盘）。connections 已加密；KCV 一并写入。
func (e *Engine) BuildFiles(key []byte, snap Snapshot, kcv string) (map[string][]byte, error) {
	if !ValidKey(key) {
		return nil, errors.New("同步密钥不可用")
	}
	files := map[string][]byte{}
	connIDs := map[string]bool{}
	for _, c := range snap.Conns {
		connIDs[c.ID] = true
		c.FormatVersion = FormatVersion
		plain, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		plain = append(plain, '\n')
		rel := ConnectionPath(c.ID)
		enc, err := e.encryptConnection(rel, key, plain)
		if err != nil {
			return nil, err
		}
		files[rel] = []byte(enc)
	}
	for _, f := range snap.Folders {
		data, err := marshalJSON(f)
		if err != nil {
			return nil, err
		}
		files[FolderPath(f.ID)] = data
	}
	for _, w := range snap.Workspaces {
		data, err := marshalJSON(w)
		if err != nil {
			return nil, err
		}
		files[WorkspacePath(w.ID)] = data
	}
	for _, c := range snap.Consoles {
		if !connIDs[c.Meta.ConnectionID] {
			continue
		}
		data, err := marshalJSON(c.Meta)
		if err != nil {
			return nil, err
		}
		files[ConsoleMetaPath(c.Meta.ID)] = data
		files[ConsoleBodyPath(c.Meta.ID)] = []byte(c.Body)
	}
	files[KCVFile] = []byte(strings.ToUpper(strings.TrimSpace(kcv)) + "\n")
	if _, err := os.Stat(filepath.Join(e.WorkTree, "README.md")); errors.Is(err, fs.ErrNotExist) {
		files["README.md"] = []byte(defaultReadme)
	}
	gitAttrPath := filepath.Join(e.WorkTree, ".gitattributes")
	existing, _ := os.ReadFile(gitAttrPath)
	files[".gitattributes"] = mergeGitAttributes(existing)
	return files, nil
}

const defaultReadme = `# db-g sync repository

This repository stores db-g connection profiles and SQL consoles.

- ` + "`db-g/connections/*.enc`" + ` are AES-GCM ciphertexts; do not edit them.
- ` + "`db-g/consoles/*.sql`" + ` are plain text and may contain sensitive SQL.
  Use a private repository and never put passwords in SQL.
- ` + "`db-g/key.kcv`" + ` is a Key Check Value, not a secret.
`

func mergeGitAttributes(existing []byte) []byte {
	need := []string{"*.enc -text", "*.sql text eol=lf"}
	lines := strings.Split(strings.ReplaceAll(string(existing), "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	for _, line := range lines {
		seen[strings.TrimSpace(line)] = true
	}
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	for _, line := range need {
		if !seen[line] {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// encryptConnection 对相同明文复用仓库已有密文，避免随机 nonce 产生噪音提交。
func (e *Engine) encryptConnection(rel string, key, plain []byte) (string, error) {
	path := filepath.Join(e.WorkTree, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Encrypt(key, plain)
	}
	if err != nil {
		return "", err
	}
	existing := string(data)
	if !IsEncrypted(existing) {
		return "", fmt.Errorf("仓库中的 %s 不是同步密文", rel)
	}
	old, err := Decrypt(key, existing)
	if err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	if string(old) == string(plain) {
		return existing, nil
	}
	return Encrypt(key, plain)
}

func marshalJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// WriteStats 是一次写树的计数。
type WriteStats struct {
	Written   int
	Unchanged int
	Deleted   int
}

// WriteFiles 原子写文件，并删除本地已不存在的实体文件（不碰未知文件）。
func (e *Engine) WriteFiles(files map[string][]byte) (WriteStats, error) {
	var st WriteStats
	for rel, data := range files {
		target := filepath.Join(e.WorkTree, filepath.FromSlash(rel))
		old, err := os.ReadFile(target)
		if err == nil && string(old) == string(data) {
			st.Unchanged++
			continue
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return st, err
		}
		if err := writeFileAtomic(target, data, 0o644); err != nil {
			return st, err
		}
		st.Written++
	}
	for _, dir := range []string{"connections", "folders", "workspaces", "consoles"} {
		root := filepath.Join(e.WorkTree, filepath.FromSlash(AppDir), dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(e.WorkTree, path)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !IsEntity(rel) {
				return nil
			}
			if _, ok := files[rel]; ok {
				return nil
			}
			if rerr := os.Remove(path); rerr == nil {
				st.Deleted++
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return st, err
		}
	}
	return st, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".db-g-sync-*.tmp")
	if err != nil {
		return os.WriteFile(path, data, mode)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return os.WriteFile(path, data, mode)
	}
	return nil
}

// ReadTree 读取工作树（合并后）的全部实体；任一连接解密失败即整体失败。
func (e *Engine) ReadTree(key []byte) (Snapshot, int, error) {
	var snap Snapshot
	root := filepath.Join(e.WorkTree, filepath.FromSlash(AppDir))
	readDir := func(dir string) ([]fs.DirEntry, error) {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return entries, err
	}
	conns, err := readDir("connections")
	if err != nil {
		return snap, 0, err
	}
	for _, ent := range conns {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".enc") {
			continue
		}
		rel := AppDir + "/connections/" + ent.Name()
		data, err := os.ReadFile(filepath.Join(root, "connections", ent.Name()))
		if err != nil {
			return snap, 0, err
		}
		plain, err := Decrypt(key, string(data))
		if err != nil {
			return snap, 0, fmt.Errorf("%s: %w", rel, err)
		}
		var c Conn
		if err := json.Unmarshal(plain, &c); err != nil {
			return snap, 0, fmt.Errorf("%s: %w", rel, err)
		}
		id := strings.TrimSuffix(ent.Name(), ".enc")
		if c.ID != id {
			return snap, 0, fmt.Errorf("%s: 连接 id 与文件名不一致", rel)
		}
		snap.Conns = append(snap.Conns, c)
	}
	folders, err := readDir("folders")
	if err != nil {
		return snap, 0, err
	}
	for _, ent := range folders {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		var f Folder
		if err := readJSON(filepath.Join(root, "folders", ent.Name()), &f); err != nil {
			return snap, 0, err
		}
		snap.Folders = append(snap.Folders, f)
	}
	spaces, err := readDir("workspaces")
	if err != nil {
		return snap, 0, err
	}
	for _, ent := range spaces {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		var w Workspace
		if err := readJSON(filepath.Join(root, "workspaces", ent.Name()), &w); err != nil {
			return snap, 0, err
		}
		snap.Workspaces = append(snap.Workspaces, w)
	}
	consoles, err := readDir("consoles")
	if err != nil {
		return snap, 0, err
	}
	connSet := map[string]bool{}
	for _, c := range snap.Conns {
		connSet[c.ID] = true
	}
	skipped := 0
	for _, ent := range consoles {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".meta.json") {
			continue
		}
		var meta ConsoleMeta
		if err := readJSON(filepath.Join(root, "consoles", ent.Name()), &meta); err != nil {
			return snap, 0, err
		}
		if !connSet[meta.ConnectionID] {
			skipped++
			continue
		}
		body, _ := os.ReadFile(filepath.Join(root, "consoles", meta.ID+".sql"))
		snap.Consoles = append(snap.Consoles, Console{Meta: meta, Body: string(body)})
	}
	sortSnapshot(&snap)
	return snap, skipped, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func sortSnapshot(s *Snapshot) {
	sort.Slice(s.Conns, func(i, j int) bool { return s.Conns[i].ID < s.Conns[j].ID })
	sort.Slice(s.Folders, func(i, j int) bool { return s.Folders[i].ID < s.Folders[j].ID })
	sort.Slice(s.Workspaces, func(i, j int) bool { return s.Workspaces[i].ID < s.Workspaces[j].ID })
	sort.Slice(s.Consoles, func(i, j int) bool { return s.Consoles[i].Meta.ID < s.Consoles[j].Meta.ID })
}
