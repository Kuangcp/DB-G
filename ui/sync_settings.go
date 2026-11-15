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
	"db-g/sync"
)

// syncSection 是设置窗口的「同步」分区。
func (a *root) syncSection(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Git 同步").Bold()
		ui.Spacer(c)
		ui.Text(c, "连接信息加密，控制台 SQL 明文").FontSize(11).TextColor(t.TextMuted)
	})

	ui.Text(c, "仓库地址").FontSize(12).TextColor(t.TextMuted)
	if ui.TextInput(c, &a.syncPrefs.RepoURL).
		Placeholder("git@github.com:me/db-g-sync.git").
		Label("同步仓库地址").
		Changed() {
		a.saveSyncPrefs()
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, "分支").FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, &a.syncPrefs.Branch).
				Placeholder("main").
				Label("同步分支").
				Changed() {
				a.saveSyncPrefs()
			}
		})
		ui.Column(c).Grow(2).Gap(4).Children(func() {
			ui.Text(c, "SSH 私钥（留空使用系统 ssh 配置 / agent）").FontSize(12).TextColor(t.TextMuted)
			if ui.TextInput(c, &a.syncPrefs.SSHKeyPath).
				Placeholder("~/.ssh/id_ed25519").
				Label("SSH 私钥路径").
				Changed() {
				a.saveSyncPrefs()
			}
		})
	})
	workTree := app.SyncWorkTree(a.syncPrefsPath)
	ui.Text(c, "同步目录："+workTree).FontSize(11).
		TextColor(t.TextMuted).SingleLine().Tooltip("固定在本机配置目录，不随仓库走")
	if _, err := os.Stat(filepath.Join(workTree, ".git")); err != nil {
		ui.Text(c, "首次点「拉取」会 clone 仓库、检出配置分支并导入数据；本地改动请先「提交并推送」。").
			FontSize(11).TextColor(t.Warning)
	} else {
		ui.Text(c, "拉取会以远端为准覆盖本地配置；本地改动请先「提交并推送」。").
			FontSize(11).TextColor(t.TextMuted)
	}

	ui.Divider(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "同步密钥").FontSize(12).TextColor(t.TextMuted)
		if a.syncKeySet {
			ui.Text(c, "● 已配置").FontSize(12).TextColor(t.Success)
			if a.syncKeyKCV != "" {
				ui.Text(c, "KCV "+a.syncKeyKCV).FontSize(11).TextColor(t.TextMuted)
			}
		} else {
			ui.Text(c, "○ 未配置").FontSize(12).TextColor(t.Warning)
		}
		ui.Spacer(c)
		if a.syncKeySet {
			if ui.Button(c, "复制密钥").Clicked() {
				if key, ok := app.LoadSyncKey(a.syncPrefsPath); ok {
					c.WriteClipboard(sync.KeyString(key))
					a.syncResult = "已复制同步密钥（请妥善保存）"
				}
			}
			if ui.Button(c, "导入密钥").Clicked() {
				a.openSyncKeyModal(false)
			}
			if ui.Button(c, "清除").Clicked() {
				if err := app.ClearSyncKey(a.syncPrefsPath); err != nil {
					a.syncWarn = "清除密钥失败：" + err.Error()
				} else {
					a.syncKeySet, a.syncKeyKCV = false, ""
					a.syncResult = "已清除本机同步密钥"
				}
			}
		} else {
			if ui.Button(c, "导入密钥").Clicked() {
				a.openSyncKeyModal(false)
			}
			if ui.PrimaryButton(c, "生成密钥").Clicked() {
				a.generateSyncKey()
			}
		}
	})
	ui.Text(c, "第一台设备生成并备份密钥；后续设备必须导入同一把密钥，否则仓库里的连接无法解密。").
		FontSize(11).TextColor(t.TextMuted)
	ui.Text(c, "密钥只保存在本机，控制台 SQL 不加密。").
		FontSize(11).TextColor(t.TextMuted)

	ui.Divider(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.PrimaryButton(c, "拉取").Disabled(a.syncBusy || strings.TrimSpace(a.syncPrefs.RepoURL) == "").
			Clicked() {
			a.startSync("pull")
		}
		if ui.Button(c, "提交并推送").Disabled(a.syncBusy || strings.TrimSpace(a.syncPrefs.RepoURL) == "").
			Clicked() {
			a.startSync("push")
		}
		if a.syncBusy {
			ui.Spinner(c)
			ui.Text(c, "同步中…").FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if a.syncPrefs.LastSyncAt > 0 {
			ui.Text(c, "上次同步 "+time.UnixMilli(a.syncPrefs.LastSyncAt).Format("01-02 15:04")).
				FontSize(11).TextColor(t.TextMuted)
		}
	})
	if a.syncResult != "" {
		ui.Text(c, a.syncResult).FontSize(11).TextColor(t.Success).SingleLine().Tooltip(a.syncResult)
	}
	if a.syncWarn != "" {
		ui.Text(c, a.syncWarn).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(a.syncWarn)
	}
	ui.Text(c, "传输用系统 git：需要本机已安装 git 并能用现有 SSH 私钥访问仓库。").
		FontSize(11).TextColor(t.TextMuted)
}

func (a *root) saveSyncPrefs() {
	prefs := a.syncPrefs
	if strings.TrimSpace(prefs.Branch) == "" {
		prefs.Branch = "main"
	}
	if err := app.SaveSyncPrefs(a.syncPrefsPath, prefs); err != nil {
		a.syncWarn = "保存同步配置失败：" + err.Error()
	}
}

func (a *root) syncService() *app.SyncService {
	return app.NewSyncService(a.store, a.syncPrefsPath)
}

// reloadSyncPrefs 把磁盘上的同步配置读回内存（保留 KCV/已确认指纹等后台写入的字段）。
func (a *root) reloadSyncPrefs() {
	prefs, warn := app.LoadSyncPrefs(a.syncPrefsPath)
	a.syncPrefs = prefs
	if warn != "" {
		a.syncWarn = warn
	}
	if key, ok := app.LoadSyncKey(a.syncPrefsPath); ok {
		a.syncKeySet = true
		a.syncKeyKCV = sync.KCV(key)
	} else {
		a.syncKeySet, a.syncKeyKCV = false, ""
	}
}

// startSync 在后台执行拉取/提交；结果为 NeedsKey/Findings 时转成弹窗。
func (a *root) startSync(kind string) {
	if a.syncBusy {
		return
	}
	if strings.TrimSpace(a.syncPrefs.RepoURL) == "" {
		a.syncWarn = "请先填写仓库地址"
		return
	}
	a.saveSyncPrefs()
	if err := a.flushActive(); err != nil {
		a.syncWarn = "保存当前控制台失败，已取消同步：" + err.Error()
		return
	}
	if m := a.consoleMgr(); m != nil {
		m.FlushAll()
	}
	a.syncBusy = true
	a.syncPendingKind = kind
	a.updateSettingsWindow()
	a.syncWarn, a.syncResult = "", "同步中…"
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
	a.syncBusy = false
	defer a.updateSettingsWindow()
	a.syncResult = res.Message
	a.reloadSyncPrefs()
	switch {
	case res.NeedsKey:
		a.syncKeyRepoKCV = res.RepoKCV
		a.openSyncKeyModal(res.RepoHasKCV)
	case len(res.Findings) > 0:
		a.syncFindings = res.Findings
		a.syncFindingsOpen = true
	case res.OK:
		a.syncWarn = ""
		a.syncPendingKind = ""
		if res.Applied {
			a.refreshAfterSync()
		}
	default:
		a.syncWarn = res.Message
		a.syncPendingKind = ""
	}
}

// refreshAfterSync 丢弃旧会话与编辑器缓存，按新 store 重新加载活动工作区/控制台。
func (a *root) refreshAfterSync() {
	a.closeAllSessions()
	a.sessionErr = map[string]string{}
	a.connErr = ""
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
	a.syncKeyOpen = true
	a.syncKeyImportOnly = importOnly
	a.syncKeyInput = ""
	a.syncKeyErr = ""
}

func (a *root) generateSyncKey() {
	a.saveSyncPrefs()
	key, err := sync.NewKey()
	if err != nil {
		a.syncWarn = "生成同步密钥失败：" + err.Error()
		return
	}
	if err := a.syncService().SaveKey(key); err != nil {
		a.syncWarn = "保存同步密钥失败：" + err.Error()
		return
	}
	a.syncKeySet = true
	a.syncKeyKCV = sync.KCV(key)
	a.syncKeyOpen = false
	a.reloadSyncPrefs()
	a.updateSettingsWindow()
	a.syncResult = "已生成同步密钥；请点「复制密钥」保存备份"
	a.rerunSync()
}

func (a *root) importSyncKey() {
	a.saveSyncPrefs()
	key, err := sync.ParseKeyString(a.syncKeyInput)
	if err != nil {
		a.syncKeyErr = err.Error()
		return
	}
	if a.syncKeyRepoKCV != "" && !sync.KCVEqual(sync.KCV(key), a.syncKeyRepoKCV) {
		a.syncKeyErr = "密钥与仓库 KCV 不匹配，请确认导入的是同一把密钥"
		return
	}
	if err := a.syncService().SaveKey(key); err != nil {
		a.syncKeyErr = "保存同步密钥失败：" + err.Error()
		return
	}
	a.syncKeySet = true
	a.syncKeyKCV = sync.KCV(key)
	a.syncKeyOpen = false
	a.syncKeyInput = ""
	a.reloadSyncPrefs()
	a.updateSettingsWindow()
	a.syncResult = "同步密钥已导入"
	a.rerunSync()
}

func (a *root) rerunSync() {
	kind := a.syncPendingKind
	if kind == "" {
		return
	}
	a.syncPendingKind = ""
	a.startSync(kind)
}

// syncKeyModal 是缺失/导入同步密钥时的弹窗。
func (a *root) syncKeyModal(c *ui.Context) {
	if !a.syncKeyOpen {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &a.syncKeyOpen, func() {
		ui.Text(c, "同步密钥").Bold()
		if a.syncKeyImportOnly {
			ui.Text(c, "仓库已有加密数据，只能导入同一把密钥；不能在这里新建密钥。").
				FontSize(12).TextColor(t.Danger)
		} else {
			ui.Text(c, "仓库还没有加密数据：可以生成新密钥，或导入已有密钥。").
				FontSize(12).TextColor(t.TextMuted)
		}
		if a.syncKeyRepoKCV != "" {
			ui.Text(c, "仓库 KCV："+a.syncKeyRepoKCV).FontSize(11).TextColor(t.TextMuted)
		}
		ui.TextInput(c, &a.syncKeyInput).
			Placeholder("粘贴 32 字节密钥的 base64 或 64 位十六进制").
			Label("同步密钥").
			Password().
			AutoFocus().
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
					a.importSyncKey()
					return true
				}
				return false
			})
		if a.syncKeyErr != "" {
			ui.Text(c, a.syncKeyErr).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(a.syncKeyErr)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.syncKeyOpen = false
				a.syncPendingKind = ""
			}
			if !a.syncKeyImportOnly {
				if ui.Button(c, "生成新密钥").Clicked() {
					a.generateSyncKey()
				}
			}
			if ui.PrimaryButton(c, "导入").Clicked() {
				a.importSyncKey()
			}
		})
	}).Width(620).Height(300)
	if m.Dismissed() {
		a.syncPendingKind = ""
	}
}

// syncFindingsModal 展示即将提交的 SQL 中疑似明文凭据，二次确认后才继续。
func (a *root) syncFindingsModal(c *ui.Context) {
	if !a.syncFindingsOpen {
		return
	}
	t := c.Theme()
	ui.Modal(c, &a.syncFindingsOpen, func() {
		ui.Text(c, "提交前发现疑似明文凭据").Bold()
		ui.Text(c, "控制台 SQL 会以明文进入仓库；确认这些不是真实密码后再继续。").
			FontSize(12).TextColor(t.Warning)
		ui.Scroll(c).Height(300).Children(func() {
			for _, f := range a.syncFindings {
				label := secretRuleLabel(f)
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, label).FontSize(10.5).TextColor(t.Danger)
					ui.Text(c, f.Path+":"+strconv.Itoa(f.Line)).FontSize(10.5).TextColor(t.TextMuted)
					ui.Text(c, f.Preview).FontSize(11).SingleLine().Tooltip(f.Preview).Grow(1)
				})
			}
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消提交").Clicked() {
				a.syncFindingsOpen = false
				a.syncFindings = nil
				a.syncPendingKind = ""
			}
			if ui.PrimaryButton(c, "仍然提交").Clicked() {
				ids := make([]string, 0, len(a.syncFindings))
				for _, f := range a.syncFindings {
					ids = append(ids, f.Fingerprint)
				}
				if err := a.syncService().AcceptFindings(ids); err != nil {
					a.syncWarn = "保存确认记录失败：" + err.Error()
					return
				}
				a.reloadSyncPrefs()
				a.syncFindingsOpen = false
				a.syncFindings = nil
				a.rerunSync()
			}
		})
	}).Width(820).Height(520)
}

func secretRuleLabel(f app.SecretFinding) string {
	name := map[string]string{
		"private-key":     "私钥",
		"url-credentials": "URL 凭据",
		"cli-password":    "命令行密码",
		"ddl-password":    "账号密码",
		"known-key":       "密钥前缀",
		"assignment":      "赋值字段",
		"insert-secret":   "INSERT 敏感列",
	}[f.Rule]
	if name == "" {
		name = f.Rule
	}
	if f.Severity == "high" {
		return name
	}
	return name + "（中）"
}
