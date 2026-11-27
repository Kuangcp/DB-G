package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/sync"
)

func TestSyncSectionShowsControls(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.gitSync.Prefs = app.DefaultSyncPrefs()
	a.gitSync.Prefs.RepoURL = "git@example.com:me/db-g-sync.git"
	a.gitSync.KeySet, a.gitSync.KeyKCV = false, ""
	tt := openSettingsSection(t, a, "同步")
	for _, want := range []string{
		"Git 同步", "仓库地址", "分支", "SSH 私钥", "同步密钥", "○ 未配置",
		"导入密钥", "生成密钥", "拉取", "提交并推送", "控制台 SQL 明文",
	} {
		if !tt.HasText(want) {
			t.Fatalf("sync section missing %q: %q", want, tt.Texts())
		}
	}
}

func TestSyncGenerateKeyPersists(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	if err := app.SaveSyncPrefs(a.gitSync.PrefsPath, app.DefaultSyncPrefs()); err != nil {
		t.Fatal(err)
	}
	a.generateSyncKey()
	if !a.gitSync.KeySet || a.gitSync.KeyKCV == "" {
		t.Fatalf("key state: %v %q", a.gitSync.KeySet, a.gitSync.KeyKCV)
	}
	key, ok := app.LoadSyncKey(a.gitSync.PrefsPath)
	if !ok || sync.KCV(key) != a.gitSync.KeyKCV {
		t.Fatalf("key file: %v", ok)
	}
	prefs, _ := app.LoadSyncPrefs(a.gitSync.PrefsPath)
	if prefs.KeyKCV != a.gitSync.KeyKCV {
		t.Fatalf("prefs kcv: %q", prefs.KeyKCV)
	}
}

func TestSyncKeyModalForbidsGenerateWhenRepoHasKey(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.gitSync.KeyRepoKCV = "ABCDEF"
	a.openSyncKeyModal(true)
	tt := ui.NewTester(rootView(a), 1000, 700)
	tt.Frame()
	if !tt.HasText("只能导入同一把密钥") || tt.HasText("生成新密钥") {
		t.Fatalf("key modal: %q", tt.Texts())
	}
}

// 设置页里点「导入密钥」必须真的弹出输入框（弹窗曾只在主窗口渲染，设置页点了没反应）。
func TestSyncImportKeyFromSettingsOpensModal(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.gitSync.Prefs = app.DefaultSyncPrefs()
	a.gitSync.Prefs.RepoURL = "git@example.com:me/db-g-sync.git"
	a.gitSync.KeySet, a.gitSync.KeyKCV = false, ""
	tt := openSettingsSection(t, a, "同步")
	if err := tt.Click("导入密钥"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.gitSync.KeyOpen {
		t.Fatal("import button must open key modal")
	}
	for _, want := range []string{"仓库还没有加密数据：可以生成新密钥，或导入已有密钥。", "导入"} {
		if !tt.HasText(want) {
			t.Fatalf("key modal missing %q: %q", want, tt.Texts())
		}
	}
}

func TestSyncFindingsModalCancelKeepsNothing(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.gitSync.Findings = []app.SecretFinding{{
		Path: "db-g/consoles/a.sql", Line: 3, Rule: "assignment", Severity: "medium",
		Preview: "password = 'ab***yz'", Fingerprint: "fp1",
	}}
	a.gitSync.FindingsOpen = true
	tt := ui.NewTester(rootView(a), 1100, 760)
	tt.Frame()
	for _, want := range []string{"提交前发现疑似明文凭据", "ab***yz", "取消提交", "仍然提交"} {
		if !tt.HasText(want) {
			t.Fatalf("findings modal missing %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("取消提交"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.gitSync.FindingsOpen {
		t.Fatal("modal should close")
	}
	prefs, _ := app.LoadSyncPrefs(a.gitSync.PrefsPath)
	if len(prefs.AcceptedFindings) != 0 {
		t.Fatalf("cancel must not accept findings: %+v", prefs.AcceptedFindings)
	}
}

func TestSyncBranchCanBeCleared(t *testing.T) {
	a := newConsoleRoot(t)
	a.gitSync.PrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.gitSync.Prefs = app.DefaultSyncPrefs()
	a.gitSync.Prefs.Branch = ""
	a.saveSyncPrefs()
	if a.gitSync.Prefs.Branch != "" {
		t.Fatalf("input branch was forced to %q", a.gitSync.Prefs.Branch)
	}
	prefs, _ := app.LoadSyncPrefs(a.gitSync.PrefsPath)
	if prefs.Branch != "main" {
		t.Fatalf("persisted branch = %q", prefs.Branch)
	}
}
