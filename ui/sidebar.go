package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"github.com/egoist/mygo/ui"

	"db-g/app"
)

func (a *root) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
		ui.Toolbar(c, func() {
			if iconButton(c, iconPlug, "新建连接", ui.Transparent).Clicked() {
				a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, "", "", "", ""
				a.connFormKind, a.connFormHost, a.connFormPort = "sqlite", "", ""
				a.connFormUser, a.connFormPass, a.connFormDB = "", "", ""
				a.resetConnTest()
			}
			if iconButton(c, iconRefresh, "刷新数据源", ui.Transparent).Clicked() {
				a.reloadTree()
				a.checkConnHealth()
			}
			ui.Spacer(c)
		}).Label("数据源操作")
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
	isActive := act.ID == p.ID && a.sess != nil
	ui.Row(c).Gap(6).Children(func() {
		dot := ui.Text(c, "●")
		status := a.connStatusOf(p.ID)
		switch {
		case isActive:
			dot.TextColor(c.Theme().Success)
		case a.connErr != "" && a.failedConnID == p.ID:
			dot.TextColor(c.Theme().Danger)
		case status == "ok":
			dot.TextColor(c.Theme().Success)
		case len(status) > 4 && status[:4] == "fail":
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
			a.toggleExpandConn(p)
		}
		ui.Spacer(c)
		if iconButtonPlain(c, iconSquarePen, "编辑连接", ui.Transparent).Clicked() {
			a.connFormOpen, a.connFormID, a.connFormName, a.connFormPath, a.connFormErr = true, p.ID, p.Name, p.Path, ""
			a.connFormKind = p.Kind
			a.connFormHost, a.connFormUser, a.connFormPass, a.connFormDB = p.Host, p.User, p.Password, p.Database
			a.connFormPort = ""
			if p.Port > 0 {
				a.connFormPort = strconv.Itoa(p.Port)
			}
			a.resetConnTest()
		}
		if iconButtonPlain(c, iconTrash, "删除连接", c.Theme().Danger).Clicked() {
			a.connDeleting, a.connDeleteOpen = p.ID, true
		}
	})
	if a.treeExpand[p.ID] {
		a.consoleSection(c, p)
		a.objectGroups(c, p)
	}
}

// consoleSection 列出该数据源的全部控制台（打开与否两种状态）并提供新建入口。
func (a *root) consoleSection(c *ui.Context, p app.ConnProfile) {
	m := a.consoleMgr()
	if m == nil {
		return
	}
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "控制台").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if iconButtonPlain(c, iconPlus, "新建控制台", ui.Transparent).Clicked() {
			rec, err := m.Create(p.ID, "")
			if err != nil {
				a.consoleErr = "新建控制台失败: " + err.Error()
				return
			}
			a.pendingActivate = rec.ID
		}
	})
	for _, rec := range m.ConsolesOf(p.ID) {
		a.consoleRow(c, rec, m.IsOpen(rec.ID))
	}
}

// consoleRow 渲染一个控制台：已打开加粗，点击打开或激活，右键关闭/重命名/删除。
func (a *root) consoleRow(c *ui.Context, rec app.ConsoleRecord, isOpen bool) {
	ui.Row(c).Gap(4).Children(func() {
		dot := ui.Text(c, "●")
		if isOpen {
			dot.TextColor(c.Theme().Success)
		} else {
			dot.TextColor(c.Theme().TextMuted)
		}
		btn := ui.Button(c, rec.Name)
		if isOpen {
			btn.Bold()
		} else {
			btn.TextColor(c.Theme().TextMuted)
		}
		if btn.Clicked() {
			a.pendingActivate = rec.ID
		}
		btn.ContextMenu(func(menu *ui.Menu) {
			if menu.Item("关闭").Disabled(!isOpen).Chosen() {
				a.pendingClose = rec.ID
			}
			if menu.Item("重命名…").Chosen() {
				a.openModal("rename", rec.ID, rec.Name)
			}
			if menu.Item("删除…").Chosen() {
				a.openModal("delete", rec.ID, rec.Name)
			}
		})
	})
}

// 连接弹窗的固定尺寸：切换数据源时表单行数不同，面板却不变，窗口不会一弹一弹。
const (
	connFormWidth  = 640
	connFormHeight = 420
)

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
		// 左右布局：左侧数据源列表（无按钮外观、行间分隔线、选中高亮），右侧表单。
		ui.Row(c).Fill().Gap(0).Children(func() {
			ui.Column(c).Width(168).FillHeight().Children(func() {
				for _, k := range connKinds {
					a.connKindRow(c, k)
				}
				ui.Spacer(c)
			})
			ui.Box(c).Width(1).FillHeight().Background(c.Theme().Border)
			ui.Column(c).Fill().Padding(0, 16).Gap(8).Children(func() {
				ui.Text(c, kindLabel(a.connFormKind)+" 连接").Bold()
				ui.TextInput(c, &a.connFormName).Placeholder("名称").AutoFocus()
				if a.connFormKind != "sqlite" {
					ui.TextInput(c, &a.connFormDB).Placeholder("数据库（可选）")
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormHost).Placeholder("主机").Fill()
						ui.TextInput(c, &a.connFormPort).Placeholder("端口")
					})
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormUser).Placeholder("用户").Fill()
						ui.TextInput(c, &a.connFormPass).Placeholder("密码").Password()
					})
				} else {
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.connFormPath).Placeholder("SQLite 文件路径").Fill()
						if ui.Button(c, "浏览…").Clicked() {
							a.browsePath()
						}
					})
				}
				if a.connFormErr != "" {
					ui.Text(c, a.connFormErr).TextColor(c.Theme().Danger)
				}
				// 留白：按钮沉到面板底部，上方空着。
				ui.Spacer(c)
				if a.connTestMsg != "" {
					col := c.Theme().Success
					if a.connTestErr {
						col = c.Theme().Danger
					}
					ui.Text(c, a.connTestMsg).FontSize(12).TextColor(col).
						SingleLine().Tooltip(a.connTestMsg)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c, "测试连接").Disabled(a.connTestBusy).Clicked() {
						a.testConnForm()
					}
					if ui.Button(c, "复制").Tooltip("把连接信息按 key: value 复制到剪贴板（不含密码）").
						Clicked() {
						a.copyConnForm(c)
					}
					ui.Spacer(c)
					if ui.Button(c, "取消").Clicked() {
						a.connFormOpen = false
					}
					if ui.PrimaryButton(c, "保存").Clicked() {
						a.saveConnForm()
					}
				})
			})
		})
	}).Width(connFormWidth).Height(connFormHeight)
}

// connKindRow 是左侧数据源类型的一行：没有按钮的外观，行底下一条分隔线，
// 选中的一行用浅色高亮 + 加粗。
func (a *root) connKindRow(c *ui.Context, k connKind) {
	t := c.Theme()
	selected := a.connFormKind == k.kind
	row := ui.ButtonBase(c).Label(k.label).Padding(10, 10).Gap(10).Justify(ui.Start).AlignItems(ui.Center)
	row.BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(0.12))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if icon := connKindIcon(k.kind); icon != nil {
			ui.Icon(c, icon).FontSize(18).TextColor(connKindColor(k.kind))
		}
		txt := ui.Text(c, k.label).SingleLine()
		if selected {
			txt.Bold()
		} else {
			txt.TextColor(t.TextMuted)
		}
	})
	if row.Clicked() {
		a.switchConnKind(k.kind)
	}
}

// connKind 是连接类型导航项。
type connKind struct{ kind, label string }

var connKinds = []connKind{
	{"sqlite", "SQLite"},
	{"mysql", "MySQL"},
	{"postgres", "PostgreSQL"},
	{"clickhouse", "ClickHouse"},
}

func kindLabel(kind string) string {
	for _, k := range connKinds {
		if k.kind == kind {
			return k.label
		}
	}
	return kind
}

// switchConnKind 切换数据源类型；端口还是旧默认值（或空）时跟随新类型的默认端口。
func (a *root) switchConnKind(kind string) {
	old := a.connFormKind
	a.connFormKind = kind
	port := strings.TrimSpace(a.connFormPort)
	if port == "" || port == strconv.Itoa(defaultPort(old)) {
		if p := defaultPort(kind); p > 0 {
			a.connFormPort = strconv.Itoa(p)
		} else {
			a.connFormPort = ""
		}
	}
	a.connFormErr = ""
	a.resetConnTest()
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
		if m := a.consoleMgr(); m != nil {
			if n := len(m.ConsolesOf(a.connDeleting)); n > 0 {
				ui.Text(c, fmt.Sprintf("将同时删除该数据源的 %d 个控制台及其 .sql 文件", n)).
					TextColor(c.Theme().Danger)
			}
		}
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

// connFormProfile 把表单当前内容读成一份连接档案（只读，不落盘）：保存、测试连接
// 和复制连接信息都用它，免得三处各读一遍表单。
func (a *root) connFormProfile() (app.ConnProfile, error) {
	p := app.ConnProfile{ID: a.connFormID, Name: a.connFormName, Kind: a.connFormKind, Path: a.connFormPath}
	if a.connFormKind == "sqlite" {
		return p, nil
	}
	portText := a.connFormPort
	if portText == "" {
		portText = strconv.Itoa(defaultPort(a.connFormKind))
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return p, errPort
	}
	p.Host = a.connFormHost
	p.Port = port
	p.User = a.connFormUser
	p.Password = a.connFormPass
	p.Database = a.connFormDB
	return p, nil
}

func (a *root) saveConnForm() {
	if a.store == nil {
		a.connFormErr = "连接库不可用"
		return
	}
	p, err := a.connFormProfile()
	if err != nil {
		a.connFormErr = "端口必须是数字"
		return
	}
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
	a.resetConnTest()
	a.checkConnHealth()
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
	case a.connErr != "":
		ui.Text(c, "●").TextColor(c.Theme().Danger)
	case a.sess != nil:
		ui.Text(c, "●").TextColor(c.Theme().Success)
	default:
		ui.Text(c, "●").TextColor(c.Theme().TextMuted)
	}
}

// defaultPort 返回各数据源的默认端口。
func defaultPort(kind string) int {
	switch kind {
	case "mysql":
		return 3306
	case "postgres":
		return 5432
	case "clickhouse":
		return 9000
	}
	return 0
}
