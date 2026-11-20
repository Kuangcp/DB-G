package ui

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/locales"
)

// exportProfiles 导出连接档案（不含密码）。
func (a *root) exportProfiles(path string) (int, error) {
	if a.store == nil {
		return 0, errors.New(locales.ConnStoreUnavailable())
	}
	n, err := app.ExportProfiles(path, a.store.List(), false)
	if err == nil {
		app.LogInfo("export", "profiles", "path", path, "count", n)
	}
	return n, err
}

// importProfiles 导入连接档案（去重 + 冲突 id 重映射），返回新增数量。
func (a *root) importProfiles(path string) (int, error) {
	if a.store == nil {
		return 0, errors.New(locales.ConnStoreUnavailable())
	}
	added, err := app.ImportProfiles(path, a.store.List())
	if err != nil {
		app.LogWarn("import", "profiles failed", "path", path, "err", err.Error())
		return 0, err
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
	app.LogInfo("import", "profiles", "path", path, "count", n)
	return n, nil
}

// pickExportProfiles 弹保存框导出连接档案。
func (a *root) pickExportProfiles() {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: locales.ProfileExport(), DefaultPath: "connections.json"})
		if err != nil || path == "" {
			return
		}
		n, err := a.exportProfiles(path)
		a.safeUpdate(win, func() {
			if err != nil {
				a.consoleErr = locales.ProfileExportFailed(err.Error())
				return
			}
			a.consoleErr = locales.ProfileExported(n, path)
		})
	}()
}

// pickImportProfiles 弹选择框导入连接档案。
func (a *root) pickImportProfiles() {
	win := a.win
	if win == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: locales.ProfileImport()})
		if err != nil || len(paths) == 0 {
			return
		}
		n, err := a.importProfiles(paths[0])
		a.safeUpdate(win, func() {
			if err != nil {
				a.consoleErr = locales.ProfileImportFailed(err.Error())
				return
			}
			a.consoleErr = locales.ProfileImported(n)
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
}
