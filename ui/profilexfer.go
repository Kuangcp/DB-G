package ui

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
)

// exportProfiles 导出连接档案（不含密码）。
func (a *root) exportProfiles(path string) (int, error) {
	if a.store == nil {
		return 0, fmt.Errorf("连接库不可用")
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
		return 0, fmt.Errorf("连接库不可用")
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
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: "导出连接", DefaultPath: "connections.json"})
		if err != nil || path == "" {
			return
		}
		n, err := a.exportProfiles(path)
		a.safeUpdate(win, func() {
			if err != nil {
				a.consoleErr = "导出连接失败: " + err.Error()
				return
			}
			a.consoleErr = fmt.Sprintf("已导出 %d 个连接 → %s", n, path)
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
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "导入连接"})
		if err != nil || len(paths) == 0 {
			return
		}
		n, err := a.importProfiles(paths[0])
		a.safeUpdate(win, func() {
			if err != nil {
				a.consoleErr = "导入连接失败: " + err.Error()
				return
			}
			a.consoleErr = fmt.Sprintf("已导入 %d 个连接", n)
		})
	}()
}

// profilesSection 渲染设置窗口里的连接档案导入/导出。
func (a *root) profilesSection(c *ui.Context) {
	cr := controlRadius(c)
	ui.Text(c, "连接档案").FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "导出连接（不含密码）").Radius(cr).Clicked() {
			a.pickExportProfiles()
		}
		if ui.Button(c, "导入连接…").Radius(cr).Clicked() {
			a.pickImportProfiles()
		}
	})
}
