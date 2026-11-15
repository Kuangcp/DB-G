package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"db-g/sync"
)

type syncDevice struct {
	dir       string
	store     *ConnStore
	service   *SyncService
	prefsPath string
	dbFile    string
}

func newSyncDevice(t *testing.T, remote string, key []byte) *syncDevice {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "connections.json")
	store := LoadStore(storePath)
	prefsPath := filepath.Join(dir, "sync.json")
	if err := SaveSyncPrefs(prefsPath, SyncPrefs{RepoURL: remote, Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	if key == nil {
		key, _ = sync.NewKey()
	}
	if err := SaveSyncKey(prefsPath, key); err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(dir, "app.db")
	if err := os.WriteFile(dbFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return &syncDevice{
		dir: dir, store: store, prefsPath: prefsPath, dbFile: dbFile,
		service: NewSyncService(store, prefsPath),
	}
}

func (d *syncDevice) addConn(t *testing.T, name, password string) ConnProfile {
	t.Helper()
	p := ConnProfile{Name: name, Kind: "sqlite", Path: d.dbFile, Password: password}
	if err := d.store.Add(p); err != nil {
		t.Fatal(err)
	}
	all := d.store.List()
	return all[len(all)-1]
}

func (d *syncDevice) addConsole(t *testing.T, connID, name, body string) ConsoleRecord {
	t.Helper()
	rec, err := d.store.CreateConsole(connID, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(d.store.ConsolesDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.store.ConsolesDir(), connID+"-"+rec.ID+".sql")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return rec
}

func syncRun(t *testing.T, svc *SyncService, action string) SyncOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return svc.Run(ctx, action)
}

func TestSyncServiceTwoDevices(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	key, _ := sync.NewKey()
	a := newSyncDevice(t, remote, key)
	b := newSyncDevice(t, remote, key)

	// 空仓库的第一次拉取应当是初始化成功，而不是“远端还没有数据”的错误。
	res := syncRun(t, a.service, "pull")
	if !res.OK || res.Applied || !strings.Contains(res.Message, "远端仓库为空") {
		t.Fatalf("initialize empty remote: %+v", res)
	}
	if cur, err := exec.Command("git", "-C", filepath.Join(a.dir, "sync", "repo"), "symbolic-ref", "--short", "HEAD").Output(); err != nil || strings.TrimSpace(string(cur)) != "main" {
		t.Fatalf("branch after init: %q %v", cur, err)
	}
	cfg, _ := os.ReadFile(filepath.Join(a.dir, "sync", "repo", ".git", "config"))
	if !strings.Contains(string(cfg), "[branch \"main\"]") {
		t.Fatalf("branch tracking missing:\n%s", cfg)
	}

	conn := a.addConn(t, "本地", "pw-1")
	console := a.addConsole(t, conn.ID, "查询", "select 1;")
	// 建一个工作区，确认跨设备同步。
	ws, err := a.store.CreateWorkspace("默认")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddWorkspaceMember(ws.ID, console.ID); err != nil {
		t.Fatal(err)
	}

	res = syncRun(t, a.service, "push")
	if !res.OK {
		t.Fatalf("push A: %+v", res)
	}
	// 仓库中的连接必须是密文，控制台正文是明文。
	encFile := filepath.Join(a.dir, "sync", "repo", "db-g", "connections", conn.ID+".enc")
	data, err := os.ReadFile(encFile)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := sync.Decrypt(key, string(data))
	if err != nil || !strings.Contains(string(plain), "pw-1") {
		t.Fatalf("connection ciphertext: %v", err)
	}
	bodyFile := filepath.Join(a.dir, "sync", "repo", "db-g", "consoles", console.ID+".sql")
	if body, err := os.ReadFile(bodyFile); err != nil || string(body) != "select 1;" {
		t.Fatalf("console plaintext: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "sync", "repo", "db-g", "key.kcv")); err != nil {
		t.Fatal("kcv missing")
	}

	res = syncRun(t, b.service, "pull")
	if !res.OK || !res.Applied {
		t.Fatalf("pull B: %+v", res)
	}
	got, ok := b.store.Get(conn.ID)
	if !ok || got.Name != "本地" || got.Password != "pw-1" {
		t.Fatalf("B connection: %+v", got)
	}
	rec, ok := b.store.GetConsole(console.ID)
	if !ok || rec.ConnectionID != conn.ID {
		t.Fatalf("B console: %+v", rec)
	}
	body, err := os.ReadFile(filepath.Join(b.store.ConsolesDir(), conn.ID+"-"+console.ID+".sql"))
	if err != nil || string(body) != "select 1;" {
		t.Fatalf("B body: %q %v", body, err)
	}
	if _, ok := b.store.GetWorkspace(ws.ID); !ok {
		t.Fatal("workspace missing on B")
	}

	// B 改控制台正文后推送，A 拉取应拿到新内容。
	bodyPath := filepath.Join(b.store.ConsolesDir(), conn.ID+"-"+console.ID+".sql")
	if err := os.WriteFile(bodyPath, []byte("select 2;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := syncRun(t, b.service, "push"); !res.OK {
		t.Fatalf("push B: %+v", res)
	}
	if res := syncRun(t, a.service, "pull"); !res.OK {
		t.Fatalf("pull A: %+v", res)
	}
	body, _ = os.ReadFile(filepath.Join(a.store.ConsolesDir(), conn.ID+"-"+console.ID+".sql"))
	if string(body) != "select 2;" {
		t.Fatalf("A body after pull: %q", body)
	}

	// A 新增连接，B 拉取后应该出现。
	conn2 := a.addConn(t, "第二个", "pw-2")
	if res := syncRun(t, a.service, "push"); !res.OK {
		t.Fatalf("push A2: %+v", res)
	}
	if res := syncRun(t, b.service, "pull"); !res.OK {
		t.Fatalf("pull B2: %+v", res)
	}
	if got, ok := b.store.Get(conn2.ID); !ok || got.Password != "pw-2" {
		t.Fatalf("B second connection: %+v", got)
	}

	// 两边各加一条连接：远端领先时提交也要先 merge 再 push，不能直接失败。
	connB := b.addConn(t, "B 新增", "pw-b")
	if res := syncRun(t, b.service, "push"); !res.OK {
		t.Fatalf("push B new: %+v", res)
	}
	connA := a.addConn(t, "A 新增", "pw-a")
	res = syncRun(t, a.service, "push")
	if !res.OK || !res.Applied {
		t.Fatalf("push A must merge remote first: %+v", res)
	}
	if _, ok := a.store.Get(connB.ID); !ok {
		t.Fatal("A should import remote connection after merge-before-push")
	}
	if res := syncRun(t, b.service, "pull"); !res.OK {
		t.Fatalf("pull B after merged push: %+v", res)
	}
	if got, ok := b.store.Get(connA.ID); !ok || got.Password != "pw-a" {
		t.Fatalf("B should see A connection: %+v", got)
	}
}

func TestSyncServiceSecretScanRequiresConfirmation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	a := newSyncDevice(t, remote, nil)
	conn := a.addConn(t, "本地", "")
	rec := a.addConsole(t, conn.ID, "危险", "ALTER USER app IDENTIFIED BY 'Sup3rPass!9';")

	res := syncRun(t, a.service, "push")
	if len(res.Findings) == 0 || res.OK {
		t.Fatalf("push must be blocked by findings: %+v", res)
	}
	found := false
	for _, f := range res.Findings {
		if strings.Contains(f.Preview, "Sup3rPass!9") {
			t.Fatalf("preview leaked: %q", f.Preview)
		}
		if f.Severity == "high" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected high severity finding: %+v", res.Findings)
	}
	ids := make([]string, 0, len(res.Findings))
	for _, f := range res.Findings {
		ids = append(ids, f.Fingerprint)
	}
	if err := a.service.AcceptFindings(ids); err != nil {
		t.Fatal(err)
	}
	res = syncRun(t, a.service, "push")
	if !res.OK {
		t.Fatalf("confirmed push: %+v", res)
	}
	_ = rec
}

func TestSyncServiceRejectsWrongKey(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	key, _ := sync.NewKey()
	a := newSyncDevice(t, remote, key)
	a.addConn(t, "本地", "pw")
	if res := syncRun(t, a.service, "push"); !res.OK {
		t.Fatalf("push: %+v", res)
	}
	other, _ := sync.NewKey()
	b := newSyncDevice(t, remote, other)
	res := syncRun(t, b.service, "pull")
	if res.OK || !strings.Contains(res.Message, "不匹配") {
		t.Fatalf("wrong key must be rejected: %+v", res)
	}
	if len(b.store.List()) != 0 {
		t.Fatal("wrong key must not import anything")
	}
}

func TestSyncPullPreservesLocalOnlyData(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	key, _ := sync.NewKey()
	a := newSyncDevice(t, remote, key)
	b := newSyncDevice(t, remote, key)

	remoteConn := a.addConn(t, "远端连接", "pw-remote")
	remoteConsole := a.addConsole(t, remoteConn.ID, "远端控制台", "select 'remote';")
	if res := syncRun(t, a.service, "push"); !res.OK {
		t.Fatalf("seed remote: %+v", res)
	}

	localConn := b.addConn(t, "本地连接", "pw-local")
	localConsole := b.addConsole(t, localConn.ID, "本地控制台", "select 'local';")

	res := syncRun(t, b.service, "pull")
	if !res.OK || !res.Applied {
		t.Fatalf("pull B: %+v", res)
	}
	if res.Preserved < 2 || !strings.Contains(res.Message, "本地独有") {
		t.Fatalf("preserved summary: %+v", res)
	}
	if _, ok := b.store.Get(remoteConn.ID); !ok {
		t.Fatal("remote connection missing after merge")
	}
	if _, ok := b.store.Get(localConn.ID); !ok {
		t.Fatal("local connection must survive pull")
	}
	if _, ok := b.store.GetConsole(remoteConsole.ID); !ok {
		t.Fatal("remote console missing after merge")
	}
	if _, ok := b.store.GetConsole(localConsole.ID); !ok {
		t.Fatal("local console must survive pull")
	}
	body, _ := os.ReadFile(filepath.Join(b.store.ConsolesDir(), localConn.ID+"-"+localConsole.ID+".sql"))
	if string(body) != "select 'local';" {
		t.Fatalf("local console body: %q", body)
	}

	// 保留的本地数据在下一次提交里共存进远端。
	if res := syncRun(t, b.service, "push"); !res.OK {
		t.Fatalf("push preserved data: %+v", res)
	}
	if res := syncRun(t, a.service, "pull"); !res.OK {
		t.Fatalf("pull A: %+v", res)
	}
	if got, ok := a.store.Get(localConn.ID); !ok || got.Password != "pw-local" {
		t.Fatalf("A should receive preserved local connection: %+v", got)
	}
}
