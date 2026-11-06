package ui

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
)

type root struct {
	code             *codeEditor
	con              *app.Console
	win              *mygo.Window
	resultState      ui.ListState
	editorPercent    float32 // 编辑器占上下的百分比（结果区在下方）
	resultsHidden    bool    // Alt+D 临时隐藏结果区
	selConn          app.ConnProfile
	selObj           engine.DBObject
	hasSelObj        bool
	sidebarWidth     float32
	store            *app.ConnStore
	connErr          string // 顶栏一行错误（最近一次连接失败的原因）
	connFormOpen     bool
	connFormID       string
	connFormName     string
	connFormPath     string
	connFormErr      string
	connFormKind     string
	connFormHost     string
	connFormPort     string
	connFormUser     string
	connFormPass     string
	connFormDB       string
	connFormParams   string
	connFormTimeout  string             // 查询超时（秒，空 = 默认 30）
	connProto        int                // ClickHouse 协议：0 native / 1 http / 2 https
	connFormFolder   string             // 连接所属文件夹；空 = 根级
	chords           map[string][]chord // 动作 → 键位（见 keymap.go）
	keymapPath       string             // keymap.json 的位置（空 = 不读盘，测试用）
	editorPrefs      app.EditorPrefs    // 编辑器字号/字体族
	editorPrefsPath  string             // editor.json 的位置（空 = 不落盘）
	editorPrefsWarn  string
	themePrefs       app.ThemePrefs // 外观：模式/强调色/密度/圆角/字号
	themePath        string         // theme.json 的位置（空 = 不落盘）
	themeWarn        string
	themeSaveSeq     atomic.Uint64
	settingsSection  int    // 设置窗口选中的分区
	captureAction    string // 设置窗口正在录制键位的动作
	keymapWarn       string // 键位文件读写问题的提示
	connTestMsg      string // 新建/编辑连接弹窗里「测试连接」「复制」的一行反馈
	connTestErr      bool
	connTestBusy     bool
	connTestSeq      int // 测试连接轮次；取消或重开时递增，旧 goroutine 的结果作废
	connDeleteOpen   bool
	connDeleting     string
	treeExpand       map[string]bool
	treeOpen         map[string]*bool
	treeData         map[string]map[engine.ObjectKind][]engine.DBObject
	treeCounts       map[string]map[engine.ObjectKind]int
	treeLoading      map[string]bool
	treePageStarted  map[string]bool
	treeSchemas      map[string][]string
	treeErr          map[string]string
	treeSelected     string
	objSearch        string          // 对象树搜索词（非空时过滤连接/文件夹/已加载对象）
	folderCollapsed  map[string]bool // folderID → 收起；缺省 = 展开
	metaCache        *app.MetaCache
	metaCacheTried   map[string]bool
	metaCacheLoading map[string]bool
	metaCacheStale   map[string]bool
	metaSaveSeq      map[string]uint64
	treeGroupStale   map[string]bool // 过期缓存的分组；展开时替换首页
	treeGen          uint64          // 重载/删除时递增，丢弃旧的异步结果
	execPending      bool

	mgr                 *app.ConsoleManager
	catalog             *catalogCache
	sessions            map[string]engine.Session // connID → 会话（最多 maxOpenSessions 个）
	sessionUse          map[string]uint64         // connID → 最近使用序号（LRU）
	sessionErr          map[string]string         // connID → 上次打开失败的原因
	useSeq              uint64
	consoles            map[string]*app.Console // consoleID → 运行态
	activeConsole       string
	consoleMRU          []string // 最近使用过的控制台（Ctrl+Tab 切换用，队首最新）
	pendingActivate     string
	pendingClose        string
	pendingSwitchWS     string
	lastVer             map[string]uint64
	lastCaret           [2]int
	stmtRuns            []stmtRun // 本次执行的语句（多语句时结果区按它分 Tab）
	activeStmt          int
	lastExecSQL         string // 最近一次执行的语句文本（取更多/复制 INSERT 用）
	resView             *resultView
	fetchStates         map[string]fetchState // runtimeID → 分页状态
	resultRow           int
	resultSel           ui.Selection[int]   // 结果表选中的单元格块（按显示行号）
	lastResult          *engine.QueryResult // 上次渲染的结果指针（换结果时清空选区）
	filterOpen          bool
	filterCol           int
	filterText          string
	history             *app.History
	pendingText         string // 激活控制台后要写入的正文（对象预览用）
	pendingExec         bool   // 写入正文后是否立即执行
	editKey             string
	editPlan            *app.EditPlan
	editSets            map[string]*app.EditSet
	readonlyReason      string
	editBusy            bool
	editCellOpen        bool
	editCellRow         int
	editCellCol         int
	editCellText        string
	previewOpen         bool
	previewLines        []string
	ddlOpen             bool
	ddlTitle            string
	ddlText             string
	ddlState            *myeditor.State // DDL 弹窗的只读高亮编辑器（惰性创建）
	historyOpen         bool
	exportConfirm       bool
	exportFormat        string
	exportStatus        string
	cellViewOpen        bool
	cellViewTitle       string
	cellViewText        string
	cellViewJSON        any // 解析后的 JSON（非 JSON 为 nil）
	cellViewJSONOK      bool
	cellViewCollapsed   map[string]bool // JSON 树里折叠的节点路径
	consoleErr          string
	widthsFor           *engine.QueryResult
	widths              []float32
	computingCompletion bool
	retriggerPending    bool
	settingsRequested   bool
	settingsWin         settingsWin
	openSettingsWin     func(mygo.WindowOptions) settingsWin // 测试注入；nil = 真实窗口
	tabMenuAction       string                               // "close" / "rename" / "delete"
	tabMenuTarget       string
	modalKind           string // "" / "rename" / "delete" / "ws-new" / "ws-rename" / "ws-delete"
	modalTarget         string
	modalText           string
	modalErr            string
	folderModalKind     string // "" / "new" / "rename" / "delete"
	folderModalTarget   string
	folderModalText     string
	folderModalErr      string
}

func newRoot(text string) *root {
	a := &root{
		code:             newCodeEditor("在左侧数据源下新建或打开控制台"),
		con:              app.NewConsole(),
		editorPercent:    65,
		sidebarWidth:     280,
		treeExpand:       map[string]bool{},
		treeOpen:         map[string]*bool{},
		treeData:         map[string]map[engine.ObjectKind][]engine.DBObject{},
		treeCounts:       map[string]map[engine.ObjectKind]int{},
		treeLoading:      map[string]bool{},
		treePageStarted:  map[string]bool{},
		treeSchemas:      map[string][]string{},
		treeErr:          map[string]string{},
		folderCollapsed:  map[string]bool{},
		metaCacheTried:   map[string]bool{},
		metaCacheLoading: map[string]bool{},
		metaCacheStale:   map[string]bool{},
		metaSaveSeq:      map[string]uint64{},
		treeGroupStale:   map[string]bool{},
		connFormKind:     "sqlite",
		chords:           defaultChords(),
		editorPrefs:      app.DefaultEditorPrefs(),
		themePrefs:       app.DefaultThemePrefs(),
		sessions:         map[string]engine.Session{},
		sessionUse:       map[string]uint64{},
		sessionErr:       map[string]string{},
		consoles:         map[string]*app.Console{},
		lastVer:          map[string]uint64{},
	}
	a.con.SetOnDone(a.execDoneLogger(""))
	a.resView = newResultView()
	a.fetchStates = map[string]fetchState{}
	a.code.SetActive("", text)
	a.catalog = newCatalogCache()
	a.code.state.OnCompletion = a.onCompletion
	a.applyEditorBindings()
	a.applyEditorPrefs()
	return a
}

// NewRoot 创建交互窗口状态（加载用户连接库并恢复上次的控制台）。
func NewRoot() *root {
	return newRootWithStore(app.DefaultStorePath())
}

// newRootWithStore 用指定连接库路径建根状态，并恢复上次激活的工作区、控制台、正文与光标。
func newRootWithStore(storePath string) *root {
	r := newRoot("")
	r.keymapPath = app.DefaultKeymapPath()
	r.keymapWarn = r.loadKeymap()
	r.applyEditorBindings()
	r.editorPrefsPath = app.DefaultEditorPrefsPath()
	r.editorPrefs, r.editorPrefsWarn = app.LoadEditorPrefs(r.editorPrefsPath)
	r.applyEditorPrefs()
	r.themePath = app.DefaultThemePrefsPath()
	r.themePrefs, r.themeWarn = app.LoadThemePrefs(r.themePath)
	r.store = app.LoadStore(storePath)
	r.metaCache = app.NewMetaCache(filepath.Join(filepath.Dir(storePath), "meta"))
	r.history = app.OpenHistory(app.DefaultHistoryPath())
	r.mgr = app.NewConsoleManager(r.store, app.ConsoleFiles{Dir: r.store.ConsolesDir()})
	r.restoreSession()
	ws, _ := r.mgr.ActiveWorkspace()
	app.LogInfo("startup", "session restored", "consoles", len(r.mgr.Open()), "workspace", ws.Name, "conn_warning", r.store.Warning())
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

// consoleMgr 懒初始化控制台管理器；没有连接库（无持久化）时返回 nil。
func (a *root) consoleMgr() *app.ConsoleManager {
	if a.mgr == nil && a.store != nil {
		a.mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir()})
	}
	return a.mgr
}

// execDoneLogger 返回一次执行结束的日志回调（连接名在执行时读取，避免陈旧）。
func (a *root) execDoneLogger(consoleID string) func(*engine.QueryResult, string, time.Duration) {
	return func(res *engine.QueryResult, errMsg string, dur time.Duration) {
		// 只带 consoleID：连接名在 exec/start（UI 线程）里已记过，这里不跨线程读 root 字段。
		kv := []any{"console", consoleID, "dur_ms", dur.Milliseconds()}
		if errMsg != "" {
			app.LogError("exec", "failed", append(kv, "err", errMsg)...)
			return
		}
		rows, affected, truncated := 0, int64(0), false
		if res != nil {
			rows, affected, truncated = len(res.Rows), res.Affected, res.Truncated
		}
		app.LogInfo("exec", "done", append(kv, "rows", rows, "affected", affected, "truncated", truncated)...)
	}
}

// runtime 返回某控制台的执行运行态（懒创建，结果按控制台各自保留）。
func (a *root) runtime(id string) *app.Console {
	if c, ok := a.consoles[id]; ok {
		return c
	}
	c := app.NewConsole()
	c.SetOnDone(a.execDoneLogger(id))
	if a.win != nil {
		win := a.win
		c.SetPublisher(func(f func()) { win.Update(f) })
	}
	a.consoles[id] = c
	return c
}

// fetchState 是一个结果的分页状态：已加载行数、是否还有更多。
type fetchState struct {
	loaded int
	more   bool
}

// runtimeFor 返回某个运行态 id 的实例；空 id 用默认实例（scratch 执行/测试口）。
func (a *root) runtimeFor(id string) *app.Console {
	if id == "" {
		return a.con
	}
	return a.runtime(id)
}

// currentRuntimeID 返回当前结果对应的运行态 id（多语句时为 consoleID#i）。
func (a *root) currentRuntimeID() string {
	if len(a.stmtRuns) > 0 && a.activeStmt < len(a.stmtRuns) {
		return a.stmtRuns[a.activeStmt].runtimeID
	}
	return a.activeConsole
}

// currentRuntime 返回结果区展示的运行态：多语句时是当前语句，否则是当前控制台。
func (a *root) currentRuntime() *app.Console {
	if len(a.stmtRuns) > 0 && a.activeStmt < len(a.stmtRuns) {
		return a.runtimeFor(a.stmtRuns[a.activeStmt].runtimeID)
	}
	return a.runtimeFor(a.activeConsole)
}

// refreshStmtBackgrounds 按语句范围与状态刷新编辑器的行底色（当前/成功/失败）。
func (a *root) refreshStmtBackgrounds() {
	st := a.code.state
	if len(a.stmtRuns) == 0 || st.Buffer == nil {
		st.LineBackgrounds = nil
		return
	}
	buf := st.Buffer
	out := make([]myeditor.LineBackground, 0, len(a.stmtRuns))
	for i, run := range a.stmtRuns {
		if !run.started {
			continue
		}
		first := buf.Pos(run.stmt.Start).Line
		last := buf.Pos(max(run.stmt.End-1, run.stmt.Start)).Line
		rt := a.runtimeFor(run.runtimeID)
		var col ui.Color
		switch {
		case rt.Running():
			col = ui.RGBA(80, 100, 160, 0.20)
		case rt.Err() != "":
			col = ui.RGBA(200, 60, 60, 0.18)
		case i == a.activeStmt:
			col = ui.RGBA(80, 160, 110, 0.16)
		default:
			col = ui.RGBA(80, 160, 110, 0.07)
		}
		out = append(out, myeditor.LineBackground{First: first, Last: last, Color: col})
	}
	st.LineBackgrounds = out
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
	a.stmtRuns, a.activeStmt = nil, 0
	text, _ := m.Text(rec.ID)
	a.code.SetActive(rec.ID, text)
	start, end := m.Caret(rec.ID)
	a.code.SetSelection(start, end)
	a.activeConsole = rec.ID
	a.touchConsoleMRU(rec.ID)
	// 活动控制台的正文惰性取自编辑器：逐帧只比较版本号，落盘时才拷贝。
	m.SetTextSource(rec.ID, func() string { return a.code.Text() })
	a.lastVer[rec.ID] = a.code.Version()
	a.lastCaret = [2]int{start, end}
	a.code.state.ScrollTo(a.code.state.CursorPos().Line)
	a.runtime(rec.ID)
	return true
}

// formatActive 格式化选区（无选区时整篇）；看不懂就保持原样。
func (a *root) formatActive() {
	text := a.code.Text()
	start, end := 0, len(text)
	if s, e, has := a.code.Selection(); has {
		start, end = s, e
	}
	if start >= end {
		return
	}
	segment := text[start:end]
	formatted, err := app.FormatSQL(segment)
	if err != nil || formatted == segment || formatted == "" {
		return
	}
	if a.activeConsole != "" {
		a.syncActiveBuffer()
	}
	a.code.Replace(start, end, formatted)
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
		app.LogWarn("console", "flush failed", "console", a.activeConsole, "err", err.Error())
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
		a.refreshStmtBackgrounds()
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

// guardView 包住一帧的构建：panic 时记堆栈并在顶栏显示错误，尽量让应用继续跑。
func (a *root) guardView(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			app.LogError("panic", "view build", "err", fmt.Sprint(r), "stack", string(debug.Stack()))
			a.consoleErr = "内部错误：" + fmt.Sprint(r)
		}
	}()
	fn()
}

func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		a.guardView(func() { a.view(c) })
	}
}

// view 是一帧的真正构建（由 guardView 包裹）。
func (a *root) view(c *ui.Context) {
	a.applyTheme(c)
	{
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
		if a.pendingText != "" {
			text := a.pendingText
			a.pendingText = ""
			a.setActiveText(text)
			a.setSelection(0, 0)
		}
		if a.pendingExec {
			a.pendingExec = false
			a.execPending = true
		}
		a.dispatchKeys(c)

		a.tabModal(c)
		a.cellViewer(c)
		a.filterModal(c)
		a.exportConfirmModal(c)
		a.historyPanel(c)
		a.ddlPanel(c)
		a.editCellModal(c)
		a.previewModal(c)

		ui.Split(c, &a.sidebarWidth, func() {
			a.sidebar(c)
		}, func() {
			ui.Column(c).Fill().Padding(8).Gap(6).Children(func() {
				// 顶栏：连接状态 + 操作。工具条里的按钮悬停前没有面孔。
				ui.Toolbar(c, func() {
					connID := a.activeConnID()
					if connID != "" {
						a.connDot(c, connID)
						if name := a.activeConnName(); name != "" {
							ui.Text(c, name).Bold()
						}
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
					if iconButton(c, iconPlay, "执行 (Ctrl+Enter)", c.Theme().Success).Disabled(a.currentRuntime().Running() || a.activeSession() == nil).Clicked() {
						a.execPending = true
					}
					if iconButton(c, iconHistory, "历史", ui.Transparent).Clicked() {
						a.historyOpen = true
					}
					if iconButton(c, iconSettings, "设置", ui.Transparent).Clicked() {
						a.requestSettings()
					}
				}).Label("操作")

				// 标签条 + 工作区切换器
				a.tabBar(c)

				// 工具行：查找（编辑器自带查找栏；性能预设/信息在独立设置窗口里）
				ui.Toolbar(c, func() {
					ui.Spacer(c)
					if iconButton(c, iconSearch, "查找 (Ctrl+F)", ui.Transparent).Clicked() {
						a.code.Find()
					}
					if iconButton(c, iconFormat, "格式化 SQL", ui.Transparent).Clicked() {
						a.formatActive()
					}
					ui.Text(c, "Ctrl+F / Ctrl+Space").FontSize(12).TextColor(c.Theme().TextMuted)
				}).Label("编辑器工具")

				// 编辑器 / 结果 分屏：按窗口高度取比例，结果区可 Alt+D 隐藏。
				editor := func() {
					a.code.View(c).Label("编辑器")
					if a.execPending {
						a.execPending = false
						a.runSQL()
					}
				}
				if a.resultsHidden {
					ui.Column(c).Grow(1).Children(editor)
				} else {
					ui.SplitVerticalPercent(c, &a.editorPercent, editor, func() {
						a.resultPane(c)
					}).Grow(1).SplitReset(65)
				}
			})
		}).Fill()
	}
}
