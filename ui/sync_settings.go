package ui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
	"db-g/sync"
)

// syncSection 是设置窗口的「同步」分区。
func (a *root) syncSection(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SyncTitle()).Bold()
		ui.Spacer(c)
		ui.Text(c, locales.SyncHint()).FontSize(11).TextColor(t.TextMuted)
	})

	ui.Text(c, locales.SyncRepo()).FontSize(12).TextColor(t.TextMuted)
	if ui.TextInput(c, &a.gitSync.Prefs.RepoURL).
		Placeholder("git@github.com:me/db-g-sync.git").
		Label(locales.SyncRepoLabel()).
		Changed() {
		a.saveSyncPrefs()
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, locales.SyncBranch()).FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, &a.gitSync.Prefs.Branch).
				Placeholder("main").
				Label(locales.SyncBranchLabel()).
				Changed() {
				a.saveSyncPrefs()
			}
		})
		ui.Column(c).Grow(2).Gap(4).Children(func() {
			ui.Text(c, locales.SyncSSHKey()).FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, &a.gitSync.Prefs.SSHKeyPath).
				Placeholder("~/.ssh/id_ed25519").
				Label(locales.SyncSSHKeyLabel()).
				Changed() {
				a.saveSyncPrefs()
			}
		})
	})
	workTree := app.SyncWorkTree(a.gitSync.PrefsPath)
	ui.Text(c, locales.SyncDir(workTree)).FontSize(11).
		TextColor(t.TextMuted).SingleLine().Tooltip(locales.SyncDirHint())
	if _, err := os.Stat(filepath.Join(workTree, ".git")); err != nil {
		ui.Text(c, locales.SyncFirstPullHint()).
			FontSize(11).TextColor(t.Warning)
	} else {
		ui.Text(c, locales.SyncPullOverwriteHint()).
			FontSize(11).TextColor(t.TextMuted)
	}

	ui.Divider(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.SyncKey()).FontSize(12).TextColor(t.TextMuted)
		if a.gitSync.KeySet {
			ui.Text(c, locales.SyncKeyConfigured()).FontSize(12).TextColor(t.Success)
			if a.gitSync.KeyKCV != "" {
				ui.Text(c, "KCV "+a.gitSync.KeyKCV).FontSize(11).TextColor(t.TextMuted)
			}
		} else {
			ui.Text(c, locales.SyncKeyMissing()).FontSize(12).TextColor(t.Warning)
		}
		ui.Spacer(c)
		if a.gitSync.KeySet {
			if ui.Button(c, locales.SyncCopyKey()).Clicked() {
				if key, ok := app.LoadSyncKey(a.gitSync.PrefsPath); ok {
					c.WriteClipboard(sync.KeyString(key))
					a.gitSync.Result = locales.SyncKeyCopied()
				}
			}
			if ui.Button(c, locales.SyncImportKey()).Clicked() {
				a.openSyncKeyModal(false)
			}
			if ui.Button(c, locales.SyncClear()).Clicked() {
				if err := app.ClearSyncKey(a.gitSync.PrefsPath); err != nil {
					a.gitSync.Warn = locales.SyncClearKeyFailed(err.Error())
				} else {
					a.gitSync.KeySet, a.gitSync.KeyKCV = false, ""
					a.gitSync.Result = locales.SyncKeyCleared()
				}
			}
		} else {
			if ui.Button(c, locales.SyncImportKey()).Clicked() {
				a.openSyncKeyModal(false)
			}
			if ui.PrimaryButton(c, locales.SyncGenerateKey()).Clicked() {
				a.generateSyncKey()
			}
		}
	})
	ui.Text(c, locales.SyncKeyAdvice()).
		FontSize(11).TextColor(t.TextMuted)
	ui.Text(c, locales.SyncKeyLocalHint()).
		FontSize(11).TextColor(t.TextMuted)

	ui.Divider(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.PrimaryButton(c, locales.SyncPull()).Disabled(a.gitSync.Busy || strings.TrimSpace(a.gitSync.Prefs.RepoURL) == "").
			Clicked() {
			a.startSync("pull")
		}
		if ui.Button(c, locales.SyncCommitPush()).Disabled(a.gitSync.Busy || strings.TrimSpace(a.gitSync.Prefs.RepoURL) == "").
			Clicked() {
			a.startSync("push")
		}
		if a.gitSync.Busy {
			ui.Spinner(c)
			ui.Text(c, locales.SyncRunning()).FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if a.gitSync.Prefs.LastSyncAt > 0 {
			ui.Text(c, locales.SyncLast(time.UnixMilli(a.gitSync.Prefs.LastSyncAt).Format("01-02 15:04"))).
				FontSize(11).TextColor(t.TextMuted)
		}
	})
	if a.gitSync.Result != "" {
		ui.Text(c, a.gitSync.Result).FontSize(11).TextColor(t.Success).SingleLine().Tooltip(a.gitSync.Result)
	}
	if a.gitSync.Warn != "" {
		ui.Text(c, a.gitSync.Warn).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(a.gitSync.Warn)
	}
	ui.Text(c, locales.SyncTransportHint()).
		FontSize(11).TextColor(t.TextMuted)
}

func (a *root) saveSyncPrefs() {
	prefs := a.gitSync.Prefs
	if strings.TrimSpace(prefs.Branch) == "" {
		prefs.Branch = "main"
	}
	if err := app.SaveSyncPrefs(a.gitSync.PrefsPath, prefs); err != nil {
		a.gitSync.Warn = locales.SyncSaveFailed(err.Error())
	}
}

func (a *root) syncService() *app.SyncService {
	return app.NewSyncService(a.store, a.gitSync.PrefsPath)
}

// reloadSyncPrefs 把磁盘上的同步配置读回内存（保留 KCV/已确认指纹等后台写入的字段）。
func (a *root) reloadSyncPrefs() {
	prefs, warn := app.LoadSyncPrefs(a.gitSync.PrefsPath)
	a.gitSync.Prefs = prefs
	if warn != "" {
		a.gitSync.Warn = warn
	}
	if key, ok := app.LoadSyncKey(a.gitSync.PrefsPath); ok {
		a.gitSync.KeySet = true
		a.gitSync.KeyKCV = sync.KCV(key)
	} else {
		a.gitSync.KeySet, a.gitSync.KeyKCV = false, ""
	}
}

// startSync 在后台执行拉取/提交；结果为 NeedsKey/Findings 时转成弹窗。
func (a *root) startSync(kind string) {
	if a.gitSync.Busy {
		return
	}
	if strings.TrimSpace(a.gitSync.Prefs.RepoURL) == "" {
		a.gitSync.Warn = locales.SyncRepoRequired()
		return
	}
	a.saveSyncPrefs()
	if err := a.flushActive(); err != nil {
		a.gitSync.Warn = locales.SyncSaveConsoleFailed(err.Error())
		return
	}
	if m := a.consoleMgr(); m != nil {
		m.FlushAll()
	}
	a.gitSync.Busy = true
	a.gitSync.PendingKind = kind
	a.updateSettingsWindow()
	a.gitSync.Warn, a.gitSync.Result = "", locales.SyncRunning()
	svc := a.syncService()
	run := func() app.SyncOutcome {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		return svc.Run(ctx, kind)
	}
	if a.win == nil {
		a.finishSync(run())
		return
	}
	win := a.win
	go func() {
		res := run()
		win.Update(func() { a.finishSync(res) })
	}()
}

func (a *root) finishSync(res app.SyncOutcome) {
	a.gitSync.Busy = false
	defer a.updateSettingsWindow()
	a.gitSync.Result = res.Message
	a.reloadSyncPrefs()
	switch {
	case res.NeedsKey:
		a.gitSync.KeyRepoKCV = res.RepoKCV
		a.openSyncKeyModal(res.RepoHasKCV)
	case len(res.Findings) > 0:
		a.gitSync.Findings = res.Findings
		a.gitSync.FindingsOpen = true
	case res.OK:
		a.gitSync.Warn = ""
		a.gitSync.PendingKind = ""
		if res.Applied {
			a.refreshAfterSync()
		}
	default:
		a.gitSync.Warn = res.Message
		a.gitSync.PendingKind = ""
	}
}

// refreshAfterSync 丢弃旧会话与编辑器缓存，按新 store 重新加载活动工作区/控制台。
func (a *root) refreshAfterSync() {
	a.closeAllSessions()
	a.sessionErr = map[string]string{}
	for _, id := range append([]string(nil), a.viewerOrder...) {
		a.closeViewerTab(id)
	}
	a.reloadTree()
	a.catalog = newCatalogCache()
	if m := a.consoleMgr(); m != nil {
		m.ResetForSync()
	}
	a.clearEditorConsole()
	a.lastVer = map[string]uint64{}
	a.runsByConsole = nil
	a.stmtRuns, a.activeStmt = nil, 0
	a.selConn, a.selObj, a.hasSelObj = app.ConnProfile{}, engine.DBObject{}, false
	if m := a.consoleMgr(); m != nil {
		if rec, ok := m.Active(); ok {
			a.activateConsole(rec.ID)
		}
	}
}

func (a *root) openSyncKeyModal(importOnly bool) {
	a.gitSync.KeyOpen = true
	a.gitSync.KeyImportOnly = importOnly
	a.gitSync.KeyInput = ""
	a.gitSync.KeyErr = ""
}

func (a *root) generateSyncKey() {
	a.saveSyncPrefs()
	key, err := sync.NewKey()
	if err != nil {
		a.gitSync.Warn = locales.SyncGenKeyFailed(err.Error())
		return
	}
	if err := a.syncService().SaveKey(key); err != nil {
		a.gitSync.Warn = locales.SyncSaveKeyFailed(err.Error())
		return
	}
	a.gitSync.KeySet = true
	a.gitSync.KeyKCV = sync.KCV(key)
	a.gitSync.KeyOpen = false
	a.reloadSyncPrefs()
	a.updateSettingsWindow()
	a.gitSync.Result = locales.SyncKeyGenerated()
	a.rerunSync()
}

func (a *root) importSyncKey() {
	a.saveSyncPrefs()
	key, err := sync.ParseKeyString(a.gitSync.KeyInput)
	if err != nil {
		a.gitSync.KeyErr = err.Error()
		return
	}
	if a.gitSync.KeyRepoKCV != "" && !sync.KCVEqual(sync.KCV(key), a.gitSync.KeyRepoKCV) {
		a.gitSync.KeyErr = locales.SyncKeyKCVBad()
		return
	}
	if err := a.syncService().SaveKey(key); err != nil {
		a.gitSync.KeyErr = locales.SyncSaveKeyFailed(err.Error())
		return
	}
	a.gitSync.KeySet = true
	a.gitSync.KeyKCV = sync.KCV(key)
	a.gitSync.KeyOpen = false
	a.gitSync.KeyInput = ""
	a.reloadSyncPrefs()
	a.updateSettingsWindow()
	a.gitSync.Result = locales.SyncKeyImported()
	a.rerunSync()
}

func (a *root) rerunSync() {
	kind := a.gitSync.PendingKind
	if kind == "" {
		return
	}
	a.gitSync.PendingKind = ""
	a.startSync(kind)
}

// syncKeyModal 是缺失/导入同步密钥时的弹窗。
func (a *root) syncKeyModal(c *ui.Context) {
	if !a.gitSync.KeyOpen {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &a.gitSync.KeyOpen, func() {
		ui.Text(c, locales.SyncKeyTitle()).Bold()
		if a.gitSync.KeyImportOnly {
			ui.Text(c, locales.SyncKeyRemoteHasData()).
				FontSize(12).TextColor(t.Danger)
		} else {
			ui.Text(c, locales.SyncKeyRemoteEmpty()).
				FontSize(12).TextColor(t.TextMuted)
		}
		if a.gitSync.KeyRepoKCV != "" {
			ui.Text(c, locales.SyncRepoKCV(a.gitSync.KeyRepoKCV)).FontSize(11).TextColor(t.TextMuted)
		}
		ui.TextInput(c, &a.gitSync.KeyInput).
			Placeholder(locales.SyncKeyPlaceholder()).
			Label(locales.SyncKeyTitle()).
			Password().
			AutoFocus().
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					a.importSyncKey()
					return true
				}
				return false
			})
		if a.gitSync.KeyErr != "" {
			ui.Text(c, a.gitSync.KeyErr).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(a.gitSync.KeyErr)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.gitSync.KeyOpen = false
				a.gitSync.PendingKind = ""
			}
			if !a.gitSync.KeyImportOnly {
				if ui.Button(c, locales.SyncGenerateNew()).Clicked() {
					a.generateSyncKey()
				}
			}
			if ui.PrimaryButton(c, locales.SyncImport()).Clicked() {
				a.importSyncKey()
			}
		})
	}).Width(620).Height(300)
	if m.Dismissed() {
		a.gitSync.PendingKind = ""
	}
}

// syncFindingsModal 展示即将提交的 SQL 中疑似明文凭据，二次确认后才继续。
func (a *root) syncFindingsModal(c *ui.Context) {
	if !a.gitSync.FindingsOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &a.gitSync.FindingsOpen, func() {
		ui.Text(c, locales.SyncFindingsTitle()).Bold()
		ui.Text(c, locales.SyncFindingsHint()).
			FontSize(12).TextColor(t.Warning)
		ui.Scroll(c).Height(300).Children(func() {
			for _, f := range a.gitSync.Findings {
				label := secretRuleLabel(f)
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, label).FontSize(10.5).TextColor(t.Danger)
					ui.Text(c, f.Path+":"+strconv.Itoa(f.Line)).FontSize(10.5).TextColor(t.TextMuted)
					ui.Text(c, f.Preview).FontSize(11).SingleLine().Tooltip(f.Preview).Grow(1)
				})
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.SyncCancelCommit()).Clicked() {
				a.gitSync.FindingsOpen = false
				a.gitSync.Findings = nil
				a.gitSync.PendingKind = ""
			}
			if ui.PrimaryButton(c, locales.SyncCommitAnyway()).Clicked() {
				ids := make([]string, 0, len(a.gitSync.Findings))
				for _, f := range a.gitSync.Findings {
					ids = append(ids, f.Fingerprint)
				}
				if err := a.syncService().AcceptFindings(ids); err != nil {
					a.gitSync.Warn = locales.SyncSaveConfirmFailed(err.Error())
					return
				}
				a.reloadSyncPrefs()
				a.gitSync.FindingsOpen = false
				a.gitSync.Findings = nil
				a.rerunSync()
			}
		})
	}).Width(820).Height(520)
}

func secretRuleLabel(f app.SecretFinding) string {
	name := map[string]string{
		"private-key":     locales.SyncFindingPrivateKey(),
		"url-credentials": locales.SyncFindingURLCreds(),
		"cli-password":    locales.SyncFindingCLIPassword(),
		"ddl-password":    locales.SyncFindingDDLPassword(),
		"known-key":       locales.SyncFindingKeyPrefix(),
		"assignment":      locales.SyncFindingAssignment(),
		"insert-secret":   locales.SyncFindingInsertColumn(),
	}[f.Rule]
	if name == "" {
		name = f.Rule
	}
	if f.Severity == "high" {
		return name
	}
	return locales.SyncFindingMedium(name)
}
