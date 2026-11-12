package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// externalRoot 建一个有数据源和 ConsoleManager 的根状态，并绑定一个外部 .sql。
func externalRoot(t *testing.T, text string) (*root, app.ConnProfile, string) {
	t.Helper()
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	path := filepath.Join(t.TempDir(), "note.sql")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleDroppedFiles([]string{path})
	if !a.extPickOpen || len(a.extPickPaths) != 1 {
		t.Fatalf("拖入后应弹数据源选择: %+v", a.extPickPaths)
	}
	a.bindExternal(prof.ID)
	if a.extPickOpen {
		t.Fatal("绑定后应关闭选择弹窗")
	}
	if _, ok := a.externalRecord(a.activeConsole); !ok {
		t.Fatalf("外部控制台未激活: %q", a.activeConsole)
	}
	return a, prof, path
}

func TestDropExternalSQLBinds(t *testing.T) {
	a, prof, path := externalRoot(t, "SELECT 1;\n")
	rec, ok := a.store.GetConsoleByFilePath(path)
	if !ok || rec.ConnectionID != prof.ID || !rec.External {
		t.Fatalf("绑定记录: %+v", rec)
	}
	if got := a.code.Text(); got != "SELECT 1;\n" {
		t.Fatalf("正文: %q", got)
	}
	// 同一文件再次拖入应复用，不新建控制台。
	before := len(a.store.ListConsoles(prof.ID))
	a.handleDroppedFiles([]string{path})
	if a.extPickOpen || len(a.store.ListConsoles(prof.ID)) != before {
		t.Fatalf("重复拖入不应新建: pick=%v n=%d", a.extPickOpen, len(a.store.ListConsoles(prof.ID)))
	}
}

func TestExternalCleanChangeReloads(t *testing.T) {
	a, _, path := externalRoot(t, "SELECT 1\n")
	if err := os.WriteFile(path, []byte("SELECT 2\nSELECT 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.pollExternalFiles(true)
	if got := a.code.Text(); got != "SELECT 2\nSELECT 3\n" {
		t.Fatalf("干净缓冲应自动重载: %q", got)
	}
	if _, st, _ := a.activeExternal(); st.conflict || st.missing {
		t.Fatalf("不应冲突: %+v", st)
	}
}

func TestExternalAutosaveThenEditNoConflict(t *testing.T) {
	a, _, path := externalRoot(t, "SELECT 1\n")
	id := a.activeConsole
	a.setActiveText("SELECT 2\n")
	a.syncActiveBuffer()
	// 模拟防抖自动保存（不经过 externalAfterSave）：磁盘已是 SELECT 2。
	if err := a.consoleMgr().Flush(id); err != nil {
		t.Fatal(err)
	}
	// 保存之后用户继续输入；此时磁盘仍是上一版，不能误判成外部修改。
	a.setActiveText("SELECT 3\n")
	a.syncActiveBuffer()
	a.pollExternalFiles(true)
	if _, st, _ := a.activeExternal(); st.conflict || st.missing {
		t.Fatalf("自己的自动保存不应被判成外部修改: %+v", st)
	}
	if got := a.code.Text(); got != "SELECT 3\n" {
		t.Fatalf("本地继续编辑的内容不能丢: %q", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "SELECT 2\n" {
		t.Fatalf("轮询不应写盘: %q", data)
	}
	if err := a.flushActive(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "SELECT 3\n" {
		t.Fatalf("后续保存应写回最新内容: %q", data)
	}
}

func recID(a *root) string { return a.activeConsole }

func TestExternalConflictAndResolve(t *testing.T) {
	a, _, path := externalRoot(t, "SELECT 1\n")
	a.setActiveText("local edit\n")
	a.syncActiveBuffer() // 标记脏（正常逐帧由 syncActiveBuffer 做）
	if err := os.WriteFile(path, []byte("outside edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.pollExternalFiles(true)
	_, st, _ := a.activeExternal()
	if !st.conflict {
		t.Fatalf("本地脏 + 外部改应冲突: %+v", st)
	}
	// 冲突时不允许写盘。
	if err := a.flushActive(); err != nil {
		t.Fatalf("冲突时 flush 应静默跳过: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "outside edit\n" {
		t.Fatalf("冲突时不能覆盖磁盘: %q", data)
	}
	if got, _ := a.consoleMgr().Text(a.activeConsole); got != "local edit\n" {
		t.Fatalf("冲突缓冲应留在内存: %q", got)
	}
	// 执行/保存入口应弹处理窗。
	if !a.externalBlocked() || !a.extResolveOpen {
		t.Fatal("冲突时应阻止执行并打开处理弹窗")
	}
	if got := a.firstBlockedExternal(); got != recID(a) {
		t.Fatalf("关闭保护应命中该控制台: %q", got)
	}
	// 冲突时关闭标签应被拦住。
	a.closeConsole(a.activeConsole)
	if _, still := a.externalRecord(a.activeConsole); !still {
		t.Fatal("冲突未处理时不应关闭标签")
	}
	a.extResolveOpen = false
	a.reloadExternal()
	if got := a.code.Text(); got != "outside edit\n" {
		t.Fatalf("重新加载: %q", got)
	}
	if _, st, _ = a.activeExternal(); st.conflict || st.missing {
		t.Fatalf("重载后应清冲突: %+v", st)
	}

	// 保留本地：明确覆盖外部改动。
	a.setActiveText("mine\n")
	a.syncActiveBuffer()
	if err := os.WriteFile(path, []byte("outside 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.pollExternalFiles(true)
	a.keepLocalExternal()
	if data, _ := os.ReadFile(path); string(data) != "mine\n" {
		t.Fatalf("保留本地应写回: %q", data)
	}
	if _, st, _ := a.activeExternal(); st.conflict {
		t.Fatalf("保留本地后应清冲突: %+v", st)
	}
}

func TestExternalMissingRecreateAndSaveAs(t *testing.T) {
	a, _, path := externalRoot(t, "SELECT 1\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.pollExternalFiles(true)
	_, st, _ := a.activeExternal()
	if !st.missing {
		t.Fatalf("文件删除应标缺失: %+v", st)
	}
	a.setActiveText("recreated\n")
	a.syncActiveBuffer()
	a.recreateExternal()
	if data, err := os.ReadFile(path); err != nil || string(data) != "recreated\n" {
		t.Fatalf("重新创建: %q %v", data, err)
	}
	if _, st, _ = a.activeExternal(); st.missing {
		t.Fatalf("重建后不应缺失: %+v", st)
	}
	// 另存为：旧文件保留，记录改绑新路径。
	rec, _, _ := a.activeExternal()
	newPath := filepath.Join(t.TempDir(), "copy.sql")
	if err := a.rebindExternal(rec.ID, newPath); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "recreated\n" {
		t.Fatalf("另存为: %q %v", data, err)
	}
	got, ok := a.store.GetConsole(rec.ID)
	if !ok || got.FilePath != newPath || !got.External {
		t.Fatalf("改绑记录: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("旧文件应保留: %v", err)
	}
}

func TestExternalStartupPrunesMissing(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	path := filepath.Join(t.TempDir(), "gone.sql")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	rec, err := a.store.CreateExternalConsole(prof.ID, "gone", path)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := a.consoleMgr().EnsureWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddWorkspaceMember(ws.ID, rec.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.pruneMissingExternal()
	for _, ws := range a.store.ListWorkspaces() {
		for _, id := range ws.Members {
			if id == rec.ID {
				t.Fatal("启动时缺失的外部控制台应从工作区移除")
			}
		}
	}
}

func TestExternalDropPickerUI(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	path := filepath.Join(t.TempDir(), "ui.sql")
	if err := os.WriteFile(path, []byte("SELECT 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleDroppedFiles([]string{path})
	tt := ui.NewTester(rootView(a), 1200, 700)
	tt.Frame()
	if !tt.HasText("选择数据源") {
		t.Fatalf("缺少数据源选择弹窗: %q", tt.Texts())
	}
	if err := tt.Click("SQLite · " + prof.Name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := a.externalRecord(a.activeConsole); !ok {
		t.Fatalf("选择数据源后应绑定并激活: %q", a.activeConsole)
	}
}

func TestOpenSQLFilesFromCLI(t *testing.T) {
	a := newConsoleRoot(t)
	prof := seedConsoleConn(t, a, "本地")
	a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	path := filepath.Join(t.TempDir(), "cli.sql")
	if err := os.WriteFile(path, []byte("SELECT 42;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.OpenSQLFiles([]string{path})
	if !a.extPickOpen || len(a.extPickPaths) != 1 {
		t.Fatalf("-o 后应弹数据源选择: %+v", a.extPickPaths)
	}
	a.bindExternal(prof.ID)
	rec, ok := a.store.GetConsoleByFilePath(path)
	if !ok || rec.ConnectionID != prof.ID || !rec.External {
		t.Fatalf("绑定记录: %+v", rec)
	}
	if got := a.code.Text(); got != "SELECT 42;\n" {
		t.Fatalf("正文: %q", got)
	}
	// 已绑定文件再次通过 -o 打开：直接激活，不再弹选择。
	a.OpenSQLFiles([]string{path})
	if a.extPickOpen || a.activeConsole != rec.ID {
		t.Fatalf("重复打开不应新建/弹窗: pick=%v active=%q", a.extPickOpen, a.activeConsole)
	}
}
