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
	a.syncPrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.syncPrefs = app.DefaultSyncPrefs()
	a.syncPrefs.RepoURL = "git@example.com:me/db-g-sync.git"
	a.syncKeySet, a.syncKeyKCV = false, ""
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
	a.syncPrefsPath = filepath.Join(t.TempDir(), "sync.json")
	if err := app.SaveSyncPrefs(a.syncPrefsPath, app.DefaultSyncPrefs()); err != nil {
		t.Fatal(err)
	}
	a.generateSyncKey()
	if !a.syncKeySet || a.syncKeyKCV == "" {
		t.Fatalf("key state: %v %q", a.syncKeySet, a.syncKeyKCV)
	}
	key, ok := app.LoadSyncKey(a.syncPrefsPath)
	if !ok || sync.KCV(key) != a.syncKeyKCV {
		t.Fatalf("key file: %v", ok)
	}
	prefs, _ := app.LoadSyncPrefs(a.syncPrefsPath)
	if prefs.KeyKCV != a.syncKeyKCV {
		t.Fatalf("prefs kcv: %q", prefs.KeyKCV)
	}
}

func TestSyncKeyModalForbidsGenerateWhenRepoHasKey(t *testing.T) {
	a := newConsoleRoot(t)
	a.syncPrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.syncKeyRepoKCV = "ABCDEF"
	a.openSyncKeyModal(true)
	tt := ui.NewTester(rootView(a), 1000, 700)
	tt.Frame()
	if !tt.HasText("只能导入同一把密钥") || tt.HasText("生成新密钥") {
		t.Fatalf("key modal: %q", tt.Texts())
	}
}

func TestSyncFindingsModalCancelKeepsNothing(t *testing.T) {
	a := newConsoleRoot(t)
	a.syncPrefsPath = filepath.Join(t.TempDir(), "sync.json")
	a.syncFindings = []app.SecretFinding{{
		Path: "db-g/consoles/a.sql", Line: 3, Rule: "assignment", Severity: "medium",
		Preview: "password = 'ab***yz'", Fingerprint: "fp1",
	}}
	a.syncFindingsOpen = true
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
	if a.syncFindingsOpen {
		t.Fatal("modal should close")
	}
	prefs, _ := app.LoadSyncPrefs(a.syncPrefsPath)
	if len(prefs.AcceptedFindings) != 0 {
		t.Fatalf("cancel must not accept findings: %+v", prefs.AcceptedFindings)
	}
}
