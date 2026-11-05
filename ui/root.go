package ui

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

type root struct {
	ed             *editor
	findQuery      string
	findActive     bool
	sampler        *Sampler
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
	selStart       int
	selEnd         int
	execPending    bool

	mgr             *app.ConsoleManager
	consoles        map[string]*app.Console // consoleID → 运行态
	activeConsole   string
	pendingActivate string
	pendingClose    string
	pendingSwitchWS string
	lastVer         map[string]uint64
	lastCaret       [2]int
	pendingSel      *[2]int
	consoleErr      string
	tabMenuAction   string // "close" / "rename" / "delete"
	tabMenuTarget   string
	modalKind       string // "" / "rename" / "delete" / "ws-new" / "ws-rename" / "ws-delete"
	modalTarget     string
	modalText       string
	modalErr        string
}

func newRoot(text string, s strats) *root {
	return &root{
		ed:            NewEditor(text, s),
		sampler:       NewSampler(128),
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
}

// NewRoot 创建交互窗口状态（SCOPE 环境变量选编辑策略，加载用户连接库并恢复上次的控制台）。
func NewRoot() *root {
	return newRootWithStore(app.DefaultStorePath(), stratsFromEnv())
}

// newRootWithStore 用指定连接库路径建根状态，并恢复上次激活的工作区、控制台、正文与光标。
func newRootWithStore(storePath string, s strats) *root {
	r := newRoot("", s)
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

func stratsFromEnv() strats {
	switch os.Getenv("SCOPE") {
	case "FF":
		return strats{scopeFull, scopeFull}
	case "FV":
		return strats{scopeFull, scopeViewport}
	}
	return strats{scopeViewport, scopeViewport}
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
	a.ed.Buf.Set(text)
	a.ed.dirty = true
	a.ed.scroll = ui.ScrollState{}
	start, end := m.Caret(rec.ID)
	a.pendingSel = &[2]int{start, end}
	a.activeConsole = rec.ID
	// 活动控制台的正文惰性取自编辑器：逐帧只比较版本号，落盘时才拷贝。
	m.SetTextSource(rec.ID, func() string { return a.ed.Buf.String() })
	a.lastVer[rec.ID] = a.ed.Buf.Version()
	a.lastCaret = [2]int{start, end}
	a.selStart, a.selEnd = start, end
	a.runtime(rec.ID)
	return true
}

// flushActive 落盘当前控制台的正文与光标（切标签/关闭/执行前/退出）。
func (a *root) flushActive() error {
	if a.activeConsole == "" {
		return nil
	}
	m := a.consoleMgr()
	if m == nil {
		return nil
	}
	m.SetCaret(a.activeConsole, a.selStart, a.selEnd)
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
	if ver := a.ed.Buf.Version(); ver != a.lastVer[a.activeConsole] {
		a.lastVer[a.activeConsole] = ver
		m.TouchVersion(a.activeConsole, ver)
	}
	if cur := [2]int{a.selStart, a.selEnd}; cur != a.lastCaret {
		a.lastCaret = cur
		m.SetCaret(a.activeConsole, cur[0], cur[1])
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

func fmtDur(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}

func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		ed := a.ed
		// 帧首：同步上一帧的编辑/选区，并执行菜单排队的激活（不能在构建中途换 buffer）。
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
		ed.Update()

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
					if ui.Button(c, "执行").Disabled(a.currentRuntime().Running() || a.sess == nil || a.selStart == a.selEnd).Clicked() {
						a.execPending = true
					}
				})

				// 标签条 + 工作区切换器
				a.tabBar(c)

				// 工具行：HUD + 尺寸 + 策略 + 查找
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, fmt.Sprintf(
						"lines=%d injected=%d tokenize=%s inject=%s build(last)=%s",
						ed.lineCount(), ed.Injected,
						scopeName(ed.strat.tokenize), scopeName(ed.strat.inject),
						fmtDur(appLastBuild),
					)).FontSize(12).SingleLine()
					if ui.Button(c, "3k").Clicked() {
						ed.Buf.Set(buildSQL(3_000))
						ed.dirty = true
					}
					if ui.Button(c, "12k").Clicked() {
						ed.Buf.Set(buildSQL(12_000))
						ed.dirty = true
					}
					if ui.Button(c, "100k").Clicked() {
						ed.Buf.Set(buildSQL(100_000))
						ed.dirty = true
					}
					if ui.Button(c, "FF").Clicked() {
						ed.SetStrategy(strats{scopeFull, scopeFull})
					}
					if ui.Button(c, "FV").Clicked() {
						ed.SetStrategy(strats{scopeFull, scopeViewport})
					}
					if ui.Button(c, "VV").Clicked() {
						ed.SetStrategy(strats{scopeViewport, scopeViewport})
					}
					ui.Spacer(c)
					ui.TextInput(c, &a.findQuery).Placeholder("查找")
					if ui.Button(c, "查找").Clicked() {
						a.findActive = true
					}
					if ui.Button(c, "清除").Clicked() {
						a.findQuery, a.findActive = "", false
					}
				})

				// 编辑器 / 结果 分屏
				ui.SplitVertical(c, &a.resultsHeight, func() {
					ui.Row(c).Fill().Children(func() {
						ui.Box(c).Width(44).FillHeight().Draw(func(p *ui.Painter, r ui.Rect) {
							ed.viewH = r.H
							p.Clip(r, 0, func() {
								top := r.Y - ed.scroll.Y
								cur := ed.lines.At(ed.scroll.Y)
								for i := cur; i < ed.lines.Count(); i++ {
									y := top + ed.lines.Top(i)
									if y > r.Y+r.H {
										break
									}
									if i == cur {
										p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 16}, ui.RGBA(80, 100, 160, 0.25), 0)
									}
									p.Text(r.X+8, y, strconv.Itoa(i+1), 12, ui.RGB(130, 130, 130))
								}
							})
						})

						in := ui.TextAreaBuffer(c, ed.Buf).Label("编辑器").Fill().Font("monospace").
							TrackLines(&ed.lines).TrackScroll(&ed.scroll)
						if a.pendingSel != nil {
							in.SetTextSelection(a.pendingSel[0], a.pendingSel[1])
							a.pendingSel = nil
						}
						if a.findActive && a.findQuery != "" {
							in.TextRanges(FindAll(ed.Buf.String(), a.findQuery)...)
						} else {
							in.TextRanges(ed.allRanges...)
						}
						a.selStart, a.selEnd = in.TextSelection()
						if a.execPending {
							a.execPending = false
							a.runSQL()
						}
					})
				}, func() {
					a.resultPane(c)
				}).Grow(1)
			})
		}).Fill()
	}
}
