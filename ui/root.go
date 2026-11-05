package ui

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

type root struct {
	code           *codeEditor
	con            *app.Console
	sess           engine.Session
	sessConnID     string
	win            *mygo.Window
	resultState    ui.ListState
	resultsHeight  float32
	sidebarWidth   float32
	store          *app.ConnStore
	connName       string
	connErr        string
	activeConnID   string
	failedConnID   string
	connFormOpen   bool
	connFormID     string
	connFormName   string
	connFormPath   string
	connFormErr    string
	connFormKind   string
	connFormHost   string
	connFormPort   string
	connFormUser   string
	connFormPass   string
	connFormDB     string
	connDeleteOpen bool
	connDeleting   string
	treeExpand     map[string]bool
	treeOpen       map[string]*bool
	treeData       map[string]map[engine.ObjectKind][]engine.DBObject
	treeErr        map[string]string
	treeSelected   string
	execPending    bool

	mgr               *app.ConsoleManager
	catalog           *catalogCache
	consoles          map[string]*app.Console // consoleID → 运行态
	activeConsole     string
	pendingActivate   string
	pendingClose      string
	pendingSwitchWS   string
	lastVer           map[string]uint64
	lastCaret         [2]int
	consoleErr        string
	widthsFor         *engine.QueryResult
	widths            []float32
	settingsRequested bool
	settingsWin       settingsWin
	openSettingsWin   func(mygo.WindowOptions) settingsWin // 测试注入；nil = 真实窗口
	tabMenuAction     string                               // "close" / "rename" / "delete"
	tabMenuTarget     string
	modalKind         string // "" / "rename" / "delete" / "ws-new" / "ws-rename" / "ws-delete"
	modalTarget       string
	modalText         string
	modalErr          string
}

func newRoot(text string) *root {
	a := &root{
		code:          newCodeEditor("在左侧数据源下新建或打开控制台"),
		con:           app.NewConsole(),
		resultsHeight: 260,
		sidebarWidth:  280,
		treeExpand:    map[string]bool{},
		treeOpen:      map[string]*bool{},
		treeData:      map[string]map[engine.ObjectKind][]engine.DBObject{},
		treeErr:       map[string]string{},
		connFormKind:  "sqlite",
		consoles:      map[string]*app.Console{},
		lastVer:       map[string]uint64{},
	}
	a.code.SetActive("", text)
	a.catalog = newCatalogCache()
	a.code.state.OnCompletion = a.onCompletion
	return a
}

// NewRoot 创建交互窗口状态（加载用户连接库并恢复上次的控制台）。
func NewRoot() *root {
	return newRootWithStore(app.DefaultStorePath())
}

// newRootWithStore 用指定连接库路径建根状态，并恢复上次激活的工作区、控制台、正文与光标。
func newRootWithStore(storePath string) *root {
	r := newRoot("")
	r.store = app.LoadStore(storePath)
	r.mgr = app.NewConsoleManager(r.store, app.ConsoleFiles{Dir: r.store.ConsolesDir()})
	r.restoreSession()
	return r
}

// restoreSession 恢复上次激活的控制台；没有可恢复的则不写盘、进空态。
func (a *root) restoreSession() bool {
	m := a.consoleMgr()
	if m == nil {
		return false
	}
	rec, ok := m.Active()
	if !ok {
		return false
	}
	return a.activateConsole(rec.ID)
}

// FlushAll 退出前落盘全部控制台正文与光标。
func (a *root) FlushAll() {
	_ = a.flushActive()
	if m := a.consoleMgr(); m != nil {
		m.FlushCarets()
		m.FlushAll()
	}
}

// setSession 安装唯一的活动会话，并关闭被替换掉的旧会话（连接归属由 sessConnID 记录）。
func (a *root) setSession(connID string, s engine.Session) {
	if a.sess != nil && a.sess != s {
		_ = a.sess.Close()
	}
	a.sess, a.sessConnID = s, connID
}

// consoleMgr 懒初始化控制台管理器；没有连接库（无持久化）时返回 nil。
func (a *root) consoleMgr() *app.ConsoleManager {
	if a.mgr == nil && a.store != nil {
		a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir()})
	}
	return a.mgr
}

// runtime 返回某控制台的执行运行态（懒创建，结果按控制台各自保留）。
func (a *root) runtime(id string) *app.Console {
	if c, ok := a.consoles[id]; ok {
		return c
	}
	c := app.NewConsole()
	if a.win != nil {
		win := a.win
		c.SetPublisher(func(f func()) { win.Update(f) })
	}
	a.consoles[id] = c
	return c
}

// currentRuntime 返回结果区展示的运行态：有激活控制台用它的，否则用默认实例（空态/测试口）。
func (a *root) currentRuntime() *app.Console {
	if a.activeConsole == "" {
		return a.con
	}
	return a.runtime(a.activeConsole)
}

// activateConsole 把编辑器切成该控制台的正文，并安排光标回位。
func (a *root) activateConsole(id string) bool {
	m := a.consoleMgr()
	if m == nil {
		return false
	}
	if id == a.activeConsole {
		return true
	}
	prev := a.activeConsole
	if err := a.flushActive(); err != nil {
		return false // 落盘失败不切换，避免丢编辑
	}
	// 旧控制台：flush 之后磁盘是权威，丢掉它的编辑器来源与内存正文。
	m.DetachTextSource(prev)
	rec, ok := m.OpenConsole(id)
	if !ok {
		return false
	}
	// 目标控制台：同样以磁盘为准（它上次离开时已落盘）。
	m.DetachTextSource(rec.ID)
	text, _ := m.Text(rec.ID)
	a.code.SetActive(rec.ID, text)
	start, end := m.Caret(rec.ID)
	a.code.SetSelection(start, end)
	a.activeConsole = rec.ID
	// 活动控制台的正文惰性取自编辑器：逐帧只比较版本号，落盘时才拷贝。
	m.SetTextSource(rec.ID, func() string { return a.code.Text() })
	a.lastVer[rec.ID] = a.code.Version()
	a.lastCaret = [2]int{start, end}
	a.code.state.ScrollTo(a.code.state.CursorPos().Line)
	a.runtime(rec.ID)
	return true
}

// hasSelection 报告当前是否有选区（「仅执行选中」的开关）。
func (a *root) hasSelection() bool {
	_, _, has := a.code.Selection()
	return has
}

// setActiveText 整体替换当前正文（测试与设置窗口预设用）。
func (a *root) setActiveText(text string) { a.code.SetText(text) }

// activeText 返回当前正文。
func (a *root) activeText() string { return a.code.Text() }

// setSelection 用 rune 偏移设置选区（测试用；编辑器内部是字节）。
func (a *root) setSelection(startRune, endRune int) { a.code.SetSelection(startRune, endRune) }

// flushActive 落盘当前控制台的正文与光标（切标签/关闭/执行前/退出）。
func (a *root) flushActive() error {
	if a.activeConsole == "" {
		return nil
	}
	m := a.consoleMgr()
	if m == nil {
		return nil
	}
	s, e := a.code.CaretRunes()
	m.SetCaret(a.activeConsole, s, e)
	m.FlushCarets()
	if err := m.Flush(a.activeConsole); err != nil {
		a.consoleErr = err.Error()
		return err
	}
	return nil
}

// syncActiveBuffer 把编辑器改动同步给管理器：只在文本版本变化时取 String()（O(n)），
// 只在选区变化时记光标，保持逐帧成本为常数级。
func (a *root) syncActiveBuffer() {
	if a.activeConsole == "" {
		return
	}
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if ver := a.code.Version(); ver != a.lastVer[a.activeConsole] {
		a.lastVer[a.activeConsole] = ver
		m.TouchVersion(a.activeConsole, ver)
	}
	if s, e := a.code.CaretRunes(); [2]int{s, e} != a.lastCaret {
		a.lastCaret = [2]int{s, e}
		m.SetCaret(a.activeConsole, s, e)
	}
}

// Attach 在窗口创建后接通主线程回投与原生窗口句柄。
func (a *root) Attach(win *mygo.Window) {
	a.win = win
	a.con.SetPublisher(func(f func()) { win.Update(f) })
	for _, c := range a.consoles {
		c.SetPublisher(func(f func()) { win.Update(f) })
	}
}

// View 是窗口内容入口。
func (a *root) View(c *ui.Context) {
	rootView(a)(c)
}

func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		// 帧首：同步上一帧的编辑/选区，并执行菜单排队的激活（不能在构建中途换 buffer）。
		a.maybeOpenSettings()
		a.syncActiveBuffer()
		if a.pendingClose != "" {
			id := a.pendingClose
			a.pendingClose = ""
			a.closeConsole(id)
		}
		if a.pendingSwitchWS != "" {
			id := a.pendingSwitchWS
			a.pendingSwitchWS = ""
			if m := a.consoleMgr(); m != nil {
				if _, ok := m.SetActiveWorkspace(id); ok {
					if rec, ok := m.Active(); ok {
						a.pendingActivate = rec.ID
					} else {
						a.clearEditorConsole()
					}
				}
			}
		}
		if a.pendingActivate != "" {
			id := a.pendingActivate
			a.pendingActivate = ""
			a.activateConsole(id)
		}
		if c.Shortcut(ui.Ctrl, ui.KeyEnter) || c.Shortcut(0, ui.KeyF5) {
			a.execPending = true
		}
		if c.Shortcut(ui.Ctrl, ui.KeyS) {
			_ = a.flushActive()
		}

		a.tabModal(c)

		ui.Split(c, &a.sidebarWidth, func() {
			a.sidebar(c)
		}, func() {
			ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
				// 顶栏：连接状态 + 执行
				ui.Row(c).Gap(8).Children(func() {
					a.connDot(c)
					if a.connName != "" {
						ui.Text(c, a.connName).Bold()
					}
					if a.connErr != "" {
						ui.Text(c, a.connErr).TextColor(c.Theme().Danger).SingleLine()
					}
					if a.consoleErr != "" {
						if ui.Button(c, a.consoleErr).TextColor(c.Theme().Danger).SingleLine().Clicked() {
							a.consoleErr = ""
						}
					}
					ui.Spacer(c)
					if ui.Button(c, "设置").Clicked() {
						a.requestSettings()
					}
					if ui.Button(c, "执行").Disabled(a.currentRuntime().Running() || a.sess == nil || !a.hasSelection()).Clicked() {
						a.execPending = true
					}
				})

				// 标签条 + 工作区切换器
				a.tabBar(c)

				// 工具行：查找（编辑器自带查找栏；性能预设/信息在独立设置窗口里）
				ui.Row(c).Gap(6).Children(func() {
					ui.Spacer(c)
					if ui.Button(c, "查找").Clicked() {
						a.code.Find()
					}
					ui.Text(c, "Ctrl+F / Ctrl+Space").FontSize(12).TextColor(c.Theme().TextMuted)
				})

				// 编辑器 / 结果 分屏
				ui.SplitVertical(c, &a.resultsHeight, func() {
					a.code.View(c).Label("编辑器")
					if a.execPending {
						a.execPending = false
						a.runSQL()
					}
				}, func() {
					a.resultPane(c)
				}).Grow(1)
			})
		}).Fill()
	}
}
