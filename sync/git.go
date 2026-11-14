package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoRemoteRef 表示远端还没有目标分支（空仓库），不是认证/网络失败。
var ErrNoRemoteRef = errors.New("remote branch not found")

// GitError 保留 git 的退出码与输出，供上层区分“认证失败 / 非快进 / 冲突”。
type GitError struct {
	Args []string
	Out  string
	Err  error
}

func (e *GitError) Error() string {
	if e.Out == "" {
		return "git " + strings.Join(e.Args, " ") + ": " + e.Err.Error()
	}
	return "git " + strings.Join(e.Args, " ") + ": " + e.Out
}

func (e *GitError) Unwrap() error { return e.Err }

// Git 是系统 git 的薄封装：不做业务判断，失败信息留在错误里。
type Git struct {
	Dir    string
	SSHKey string
	// Timeout 是单个 git 命令的上限；0 用 120s。
	Timeout time.Duration
}

// NewGit 创建封装。
func NewGit(dir, sshKey string) *Git { return &Git{Dir: dir, SSHKey: sshKey} }

// Available 报告系统是否有 git。
func Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "--version")
	cmd.Stdin = nil
	return cmd.Run() == nil
}

func (g *Git) timeout() time.Duration {
	if g.Timeout > 0 {
		return g.Timeout
	}
	return 120 * time.Second
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (g *Git) run(ctx context.Context, args ...string) (string, error) {
	return g.runAt(ctx, g.Dir, args...)
}

func (g *Git) runAt(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout())
	defer cancel()
	full := append([]string{"-c", "core.quotepath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true")
	if g.SSHKey != "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -i "+shellQuote(g.SSHKey)+
			" -o IdentitiesOnly=yes -o BatchMode=yes")
	}
	cmd.Env = env
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 64<<10 {
		text = text[:64<<10] + "\n…"
	}
	if err != nil {
		return text, &GitError{Args: args, Out: text, Err: err}
	}
	return text, nil
}

// Clone 把远端克隆到 g.Dir；目标必须是空目录或不存在，绝不覆盖已有内容。
func (g *Git) Clone(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("仓库地址为空")
	}
	if st, err := os.Stat(g.Dir); err == nil {
		if !st.IsDir() {
			return errors.New("同步目录已存在且不是目录")
		}
		entries, _ := os.ReadDir(g.Dir)
		if len(entries) > 0 {
			return errors.New("同步目录已有内容但不是一个 git 仓库")
		}
	}
	parent := filepath.Dir(g.Dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	_, err := g.runAt(ctx, parent, "clone", url, g.Dir)
	return err
}

// CheckoutBranch 检出配置的分支：本地已有就切换，否则从 origin/<branch> 建立；
// 远端为空时创建同名分支。
func (g *Git) CheckoutBranch(ctx context.Context, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return errors.New("分支为空")
	}
	if cur, err := g.CurrentBranch(ctx); err == nil && cur == branch {
		return nil
	}
	if _, err := g.run(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err := g.run(ctx, "checkout", branch)
		return err
	}
	if _, err := g.run(ctx, "rev-parse", "--verify", "refs/remotes/origin/"+branch); err == nil {
		_, err := g.run(ctx, "checkout", "-b", branch, "origin/"+branch)
		return err
	}
	_, err := g.run(ctx, "checkout", "-B", branch)
	return err
}

// SetIdentity 只在仓库未配置身份时写本地身份，不覆盖用户已有配置。
func (g *Git) SetIdentity(ctx context.Context) {
	if _, err := g.run(ctx, "config", "user.name"); err != nil {
		_, _ = g.run(ctx, "config", "user.name", "db-g")
	}
	if _, err := g.run(ctx, "config", "user.email"); err != nil {
		_, _ = g.run(ctx, "config", "user.email", "db-g@localhost")
	}
}

// SetRemote 设置 origin。
func (g *Git) SetRemote(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("仓库地址为空")
	}
	if _, err := g.run(ctx, "remote", "get-url", "origin"); err == nil {
		_, err = g.run(ctx, "remote", "set-url", "origin", url)
		return err
	}
	_, err := g.run(ctx, "remote", "add", "origin", url)
	return err
}

// CurrentBranch 返回当前分支名；仓库还没有提交时用 symbolic-ref 也能拿到名字。
func (g *Git) CurrentBranch(ctx context.Context) (string, error) {
	if out, err := g.run(ctx, "symbolic-ref", "--short", "HEAD"); err == nil {
		if name := strings.TrimSpace(out); name != "" {
			return name, nil
		}
	}
	out, err := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if name == "HEAD" {
		return "", nil
	}
	return name, nil
}

// SymbolicRef 把 HEAD 指向一个尚未提交的分支。
func (g *Git) SymbolicRef(ctx context.Context, ref string) error {
	_, err := g.run(ctx, "symbolic-ref", "HEAD", ref)
	return err
}

// SetBranchTracking 写入 origin/<branch> 的跟踪配置；远端 ref 还不存在时只写配置。
func (g *Git) SetBranchTracking(ctx context.Context, branch, remote string) {
	_, _ = g.run(ctx, "config", "branch."+branch+".remote", remote)
	_, _ = g.run(ctx, "config", "branch."+branch+".merge", "refs/heads/"+branch)
	_, _ = g.run(ctx, "branch", "--set-upstream-to="+remote+"/"+branch, branch)
}

// Head 返回当前提交 id；仓库为空时返回空串。
func (g *Git) Head(ctx context.Context) string {
	out, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Dirty 报告工作树是否有未提交改动。
func (g *Git) Dirty(ctx context.Context) bool {
	out, err := g.run(ctx, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// HasCommits 报告本地是否已有提交。
func (g *Git) HasCommits(ctx context.Context) bool {
	_, err := g.run(ctx, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// Fetch 拉取远端分支；远端还没有该分支时返回 ErrNoRemoteRef。
func (g *Git) Fetch(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "fetch", "--prune", "origin", branch)
	if err == nil {
		return nil
	}
	var ge *GitError
	if errors.As(err, &ge) {
		out := strings.ToLower(ge.Out)
		if strings.Contains(out, "couldn't find remote ref") ||
			strings.Contains(out, "could not find remote ref") ||
			strings.Contains(out, "no such ref") {
			return ErrNoRemoteRef
		}
	}
	return err
}

// RemoteRef 返回 origin/<branch> 的 ref；本地未 fetch 到或不存在时 ok=false。
func (g *Git) RemoteRef(ctx context.Context, branch string) (string, bool) {
	ref := "refs/remotes/origin/" + branch
	if _, err := g.run(ctx, "rev-parse", "--verify", ref); err != nil {
		return "", false
	}
	return ref, true
}

// HasMergeBase 报告两个 ref 是否有共同祖先。
func (g *Git) HasMergeBase(ctx context.Context, a, b string) bool {
	_, err := g.run(ctx, "merge-base", a, b)
	return err == nil
}

// Merge 合并 ref；冲突时返回错误，调用方用 Conflicted 取路径。
func (g *Git) Merge(ctx context.Context, ref string, allowUnrelated bool) error {
	args := []string{"merge", "--no-edit"}
	if allowUnrelated {
		args = append(args, "--allow-unrelated-histories")
	}
	args = append(args, ref)
	_, err := g.run(ctx, args...)
	return err
}

// MergeInProgress 报告是否有未完成的 merge。
func (g *Git) MergeInProgress(ctx context.Context) bool {
	out, err := g.run(ctx, "rev-parse", "--git-path", "MERGE_HEAD")
	if err != nil {
		return false
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/") {
		path = g.Dir + "/" + path
	}
	_, statErr := os.Stat(path)
	return statErr == nil
}

// MergeAbort 放弃未完成的 merge。
func (g *Git) MergeAbort(ctx context.Context) {
	_, _ = g.run(ctx, "merge", "--abort")
}

// Conflicted 返回未合并路径（相对工作树）。
func (g *Git) Conflicted(ctx context.Context) []string {
	out, err := g.run(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// Commit 暂存 db-g/ 与两个根文件并提交；没有变更时返回 false。
func (g *Git) Commit(ctx context.Context, message string) (bool, error) {
	paths := []string{AppDir, ".gitattributes", "README.md"}
	status, err := g.run(ctx, append([]string{"status", "--porcelain", "--"}, paths...)...)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) == "" {
		return false, nil
	}
	if _, err := g.run(ctx, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return false, err
	}
	if _, err := g.run(ctx, "commit", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

// Push 推送 HEAD 到 origin/<branch>。
func (g *Git) Push(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "push", "origin", "HEAD:refs/heads/"+branch)
	return err
}

// ShowFile 读取某个 ref 下的文件；不存在返回 ok=false。
func (g *Git) ShowFile(ctx context.Context, ref, path string) ([]byte, bool) {
	out, err := g.run(ctx, "show", ref+":"+path)
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// RemoteHasEntity 报告远端分支里 db-g/ 下是否有指定目录或文件。
func (g *Git) RemoteHasEntity(ctx context.Context, ref, path string) bool {
	out, err := g.run(ctx, "ls-tree", "-r", "--name-only", ref, "--", path)
	return err == nil && strings.TrimSpace(out) != ""
}

// ConflictError 把 merge 失败和冲突路径一起带回给上层。
type ConflictError struct {
	Paths []string
	Err   error
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("合并冲突：%s", strings.Join(e.Paths, ", "))
}

func (e *ConflictError) Unwrap() error { return e.Err }
