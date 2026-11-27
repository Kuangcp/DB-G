package ui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// exportProfilesTo 只把连接档案写到文件，不读 root；list 由 UI 线程传入。
func exportProfilesTo(path string, list []app.ConnProfile) (int, error) {
	n, err := app.ExportProfiles(path, list, false)
	if err == nil {
		app.LogInfo("export", "profiles", "path", path, "count", n)
	}
	return n, err
}

// importProfilesFrom 只解析文件并去重，不碰 root；existing 由 UI 线程传入。
func importProfilesFrom(path string, existing []app.ConnProfile) ([]app.ConnProfile, error) {
	added, err := app.ImportProfiles(path, existing)
	if err != nil {
		app.LogWarn("import", "profiles failed", "path", path, "err", err.Error())
		return nil, err
	}
	return added, nil
}

// applyImportedProfiles 在 UI 线程把导入结果写进连接库并刷新对象树。
func (a *root) applyImportedProfiles(added []app.ConnProfile) int {
	if a.store == nil {
		return 0
	}
	n := 0
	for _, p := range added {
		if err := a.store.Add(p); err != nil {
			app.LogWarn("import", "add failed", "name", p.Name, "err", err.Error())
			continue
		}
		n++
	}
	a.reloadTree()
	app.LogInfo("import", "profiles", "count", n)
	return n
}

// pickExportProfiles 弹保存框导出连接档案。
func (a *root) pickExportProfiles() {
	win := a.win
	if win == nil {
		return
	}
	if a.store == nil {
		a.toastError(locales.ConnStoreUnavailable())
		return
	}
	list := a.store.List()
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: locales.ProfileExport(), DefaultPath: "connections.json"})
		if err != nil || path == "" {
			return
		}
		n, err := exportProfilesTo(path, list)
		a.safeUpdate(win, func() {
			if err != nil {
				a.reportSettingsResult("profiles", locales.ProfileExportFailed(err.Error()), true)
				return
			}
			a.reportSettingsResult("profiles", locales.ProfileExported(n, path), false)
		})
	}()
}

// pickImportProfiles 弹选择框导入连接档案。
func (a *root) pickImportProfiles() {
	win := a.win
	if win == nil {
		return
	}
	if a.store == nil {
		a.toastError(locales.ConnStoreUnavailable())
		return
	}
	existing := a.store.List()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: locales.ProfileImport()})
		if err != nil || len(paths) == 0 {
			return
		}
		added, err := importProfilesFrom(paths[0], existing)
		a.safeUpdate(win, func() {
			if err != nil {
				a.reportSettingsResult("profiles", locales.ProfileImportFailed(err.Error()), true)
				return
			}
			a.reportSettingsResult("profiles", locales.ProfileImported(a.applyImportedProfiles(added)), false)
		})
	}()
}

// profilesSection 渲染设置窗口里的连接档案导入/导出。
func (a *root) profilesSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, locales.SettingsProfiles()).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, locales.ProfileExportButton()).Radius(cr).Clicked() {
			a.pickExportProfiles()
		}
		if ui.Button(c, locales.ProfileImportButton()).Radius(cr).Clicked() {
			a.pickImportProfiles()
		}
	})
	a.settingsNoticeLine(c, "profiles")
}
