package ui

import (
	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "新建连接").Clicked() {
				a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, "", "", "", ""
			}
			if ui.Button(c, "刷新").Clicked() {
				a.reloadTree()
			}
		})
		if a.store == nil {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
			a.connModal(c)
			return
		}
		if w := a.store.Warning(); w != "" {
			ui.Text(c, w).TextColor(c.Theme().Danger)
		}
		list := a.store.List()
		if len(list) == 0 {
			ui.Text(c, "左上角「新建连接」开始").TextColor(c.Theme().TextMuted)
		}
		ui.Scroll(c).Grow(1).Children(func() {
			for _, p := range list {
				a.connRow(c, p)
			}
		})
	})
	a.connModal(c)
}

func (a *root) connRow(c *ui.Context, p app.ConnProfile) {
	act, _ := a.store.Active()
	isActive := act.ID == p.ID && a.con.Session() != nil
	ui.Row(c).Gap(6).Children(func() {
		dot := ui.Text(c, "●")
		switch {
		case isActive:
			dot.TextColor(c.Theme().Success)
		case a.connErr != "" && a.connName == p.Name:
			dot.TextColor(c.Theme().Danger)
		default:
			dot.TextColor(c.Theme().TextMuted)
		}
		lbl := ui.Button(c, p.Name)
		if isActive {
			lbl.Bold()
		}
		if lbl.DoubleClicked() {
			_ = a.openConn(p)
		}
		expandLabel := "▸"
		if a.treeExpand[p.ID] {
			expandLabel = "▾"
		}
		if ui.Button(c, expandLabel).Clicked() {
			if a.treeExpand == nil {
				a.treeExpand = map[string]bool{}
			}
			a.treeExpand[p.ID] = !a.treeExpand[p.ID]
			if a.treeExpand[p.ID] {
				a.expandConn(p)
			}
		}
		ui.Spacer(c)
		if ui.Button(c, "编辑").Clicked() {
			a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, p.ID, p.Name, p.Path, ""
		}
		if ui.Button(c, "删除").Clicked() {
			a.connDeleting, a.connDeleteOpen = p.ID, true
		}
	})
	if a.treeExpand[p.ID] {
		a.objectGroups(c, p)
	}
}

func (a *root) connModal(c *ui.Context) {
	if a.connDeleteOpen {
		a.deleteModal(c)
		return
	}
	ui.Modal(c, &a.connFormOpen, func() {
		title := "新建连接"
		if a.connFormID != "" {
			title = "编辑连接"
		}
		ui.Text(c, title).Bold()
		ui.TextInput(c, &a.connFormName).Placeholder("名称").AutoFocus()
		ui.Row(c).Gap(6).Children(func() {
			ui.TextInput(c, &a.connFormPath).Placeholder("SQLite 文件路径").Fill()
			if ui.Button(c, "浏览…").Clicked() {
				a.browsePath()
			}
		})
		if a.connFormErr != "" {
			ui.Text(c, a.connFormErr).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.connFormOpen = false
			}
			if ui.PrimaryButton(c, "保存").Clicked() {
				a.saveConnForm()
			}
		})
	})
}

func (a *root) deleteModal(c *ui.Context) {
	name := ""
	for _, p := range a.store.List() {
		if p.ID == a.connDeleting {
			name = p.Name
		}
	}
	ui.Modal(c, &a.connDeleteOpen, func() {
		ui.Text(c, "删除连接「"+name+"」？").Bold()
		ui.Text(c, "仅移除配置，不删除数据库文件").TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.connDeleteOpen = false
			}
			if ui.Button(c, "删除").Clicked() {
				a.removeConn(a.connDeleting)
				a.connDeleting, a.connDeleteOpen = "", false
			}
		})
	})
}

func (a *root) saveConnForm() {
	if a.store == nil {
		a.connFormErr = "连接库不可用"
		return
	}
	p := app.ConnProfile{ID: a.connFormID, Name: a.connFormName, Kind: "sqlite", Path: a.connFormPath}
	var err error
	if a.connFormID == "" {
		err = a.store.Add(p)
	} else {
		err = a.store.Update(p)
	}
	if err != nil {
		a.connFormErr = err.Error()
		return
	}
	a.connFormOpen = false
}

func (a *root) browsePath() {
	if a.win == nil {
		return
	}
	win := a.win
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  "选择 SQLite 数据库",
			Filters: []mygo.FileFilter{
				{Name: "SQLite", Extensions: []string{"db", "sqlite", "sqlite3"}},
			},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() { a.connFormPath = paths[0] })
	}()
}

func (a *root) connDot(c *ui.Context) {
	switch {
	case a.con.Session() != nil:
		ui.Text(c, "●").TextColor(c.Theme().Success)
	case a.connErr != "":
		ui.Text(c, "●").TextColor(c.Theme().Danger)
	default:
		ui.Text(c, "●").TextColor(c.Theme().TextMuted)
	}
}
