package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
	"db-g/sync"
)

type root struct {
	code            *codeEditor
	stmtOKColor     ui.Color // 执行成功圆点（主题 Success）
	stmtErrColor    ui.Color // 执行失败圆点（主题 Danger）
	stmtRunColor    ui.Color // 执行中圆点（主题 Accent）
	ai              aiState
	con             *app.Console
	win             *mygo.Window
	grid            *gridState   // 当前展示的网格（控制台或表视图）
	resultClickMods ui.Modifiers // 结果表格最近一次指针按下/抬起时的修饰键
	pointerDouble   bool         // 最近一次指针事件是否是双击的第二次按下
	editorPercent   float32      // 编辑器占上下的百分比（结果区在下方）
	resultsHidden   bool         // Alt+D 临时隐藏结果区
	sidebarHidden   bool         // 临时隐藏左侧数据源面板（纯瞬态）
	selConn         app.ConnProfile
	selObj          engine.DBObject
	hasSelObj       bool
	sidebarWidth    float32
	store           *app.ConnStore
	toasts          toastState
	connForm        connFormState // 新建/编辑连接弹窗
	connProto       int           // ClickHouse 协议：0 native / 1 http / 2 https
	settings        settingsState // 设置窗口与全部偏好
	gitSync         syncState     // Git 同步：配置、密钥弹窗、发现项
	tree            treeState
	deferred        []func()      // 下一帧帧首执行的动作（FIFO）
	modals          modalRegistry // 每帧按顺序询问的弹窗

	appIconPNG []byte       // 窗口图标；自举安装时写入图标目录
	install    installState // 自举安装/升级（仅发布构建）

	external externalState

	session             sessionState
	tabs                consoleTabsState
	history             *app.History
	edit                editState
	modal               modalState
	exportState         exportState
	revealPath          func(string) error // 测试注入；nil = app.RevealPath
	cellView            cellViewState
	consoleErr          string
	computingCompletion bool
	retriggerPending    bool
	completionWanted    bool // 编辑器正在等/持有补全弹层（用户触发）
	completionWordStart int  // 触发时词起点的字节偏移；光标离开这个词就不再重弹
}

func newRoot(text string) *root {
	a := &root{
		code:          newCodeEditor(locales.TabEmptyHint()),
		con:           app.NewConsole(),
		editorPercent: 65,
		sidebarWidth:  280,
		tree: treeState{
			Expand:           map[string]bool{},
			Open:             map[string]*bool{},
			Data:             map[string]map[engine.ObjectKind][]engine.DBObject{},
			Counts:           map[string]map[engine.ObjectKind]int{},
			Loading:          map[string]bool{},
			PageStarted:      map[string]bool{},
			Schemas:          map[string][]string{},
			SchemaLoading:    map[string]bool{},
			ConnSchema:       map[string]string{},
			Err:              map[string]string{},
			FolderCollapsed:  map[string]bool{},
			MetaCacheTried:   map[string]bool{},
			MetaCacheLoading: map[string]bool{},
			MetaCacheStale:   map[string]bool{},
			MetaSaveSeq:      map[string]uint64{},
			GroupStale:       map[string]bool{},
		},
		connForm: connFormState{Kind: "sqlite", TagColor: app.DefaultConnTagColor},
		cellView: cellViewState{ModalW: cellModalDefaultW, ModalH: cellModalDefaultH, Wrap: true},
		settings: settingsState{
			Chords:        defaultChords(),
			MouseBindings: defaultMouseBindings(),
			FontPrefs:     app.DefaultFontPrefs(),
			ResultPrefs:   app.DefaultResultPrefs(),
			ConsolePrefs:  app.DefaultConsolePrefs(),
			ThemePrefs:    app.DefaultThemePrefs(),
		},
		session: sessionState{
			Sessions:      map[string]engine.Session{},
			Use:           map[string]uint64{},
			Err:           map[string]string{},
			Opening:       map[string]bool{},
			Waiters:       map[string][]func(error){},
			ServerInfo:    map[string]engine.ServerInfo{},
			ServerInfoErr: map[string]string{},
			ServerInfoSeq: map[string]uint64{},
		},
		tabs: consoleTabsState{
			Consoles:   map[string]*app.Console{},
			ViewerTabs: map[string]*viewerTab{},
			LastVer:    map[string]uint64{},
			PageStates: map[string]*pageState{},
		},
		external: externalState{Files: map[string]*extFile{}},
	}
	a.con.SetOnDone(a.execDoneLogger(""))
	a.con.SetPageSize(a.settings.ResultPrefs.PageSize)
	a.con.SetResultBudgetMB(a.settings.ResultPrefs.BudgetMB)
	a.grid = newGridState()
	a.tabs.ConsoleGrid = a.grid
	a.code.SetActive("", text)
	a.session.Catalog = newCatalogCache()
	a.code.state.OnCompletion = a.onCompletion
	// 编辑器里 Ctrl+点击 / F12 标识符 = Ctrl+Q：打开该表/视图的 DDL。
	a.code.state.OnDefinition = a.onEditorDefinition
	// 右键菜单末尾追加「运行」；编辑器自带的编辑项由 mygo 放在前面。
	a.code.state.ContextMenu = func(m *ui.Menu) {
		if m.Item(locales.EditorRun()).Shortcut(ui.Ctrl, ui.KeyEnter).Chosen() {
			a.runSQL()
		}
	}
	a.applyEditorBindings()
	a.applyFontPrefs()
	a.modals.add(
		a.tabModal,
		a.consoleReloadModal,
		a.cellViewer,
		a.filterModal,
		a.exportConfirmModal,
		a.exportCloseModal,
		a.historyPanel,
		a.palette,
		a.ddlPanel,
		a.statsPanel,
		a.aiLintModal,
		a.editCellModal,
		a.previewModal,
		a.installModal,
		a.externalPickerModal,
		a.externalResolveModal,
		a.aiDebugModal,
		a.aiFixModal,
		a.aiRewriteModal,
		a.syncKeyModal,
		a.syncFindingsModal,
		a.planConfirmModal,
		a.guardModal,
	)
	return a
}

// NewRoot 创建交互窗口状态（加载用户连接库并恢复上次的控制台）。
func NewRoot() *root {
	return newRootWithStore(app.DefaultStorePath())
}

// newRootWithStore 用指定连接库路径建根状态，并恢复上次激活的工作区、控制台、正文与光标。
func newRootWithStore(storePath string) *root {
	r := newRoot("")
	paths := app.ConfigPathsForStore(storePath)
	r.settings.KeymapPath = paths.Keymap()
	if warn := r.loadKeymap(); warn != "" {
		r.setSettingsNotice("keymap", warn, true)
	}
	r.applyEditorBindings()
	r.settings.FontPrefsPath = paths.Fonts()
	var fontWarn string
	r.settings.FontPrefs, fontWarn = app.LoadFontPrefs(r.settings.FontPrefsPath)
	if fontWarn != "" {
		r.setSettingsNotice("font", fontWarn, true)
	}
	r.applyFontPrefs()
	r.settings.ResultPrefsPath = paths.Results()
	var resultWarn string
	r.settings.ResultPrefs, resultWarn = app.LoadResultPrefs(r.settings.ResultPrefsPath)
	if resultWarn != "" {
		r.setSettingsNotice("result", resultWarn, true)
	}
	r.applyResultLimits()
	r.settings.ConsolePrefsPath = paths.Console()
	var consoleWarn string
	r.settings.ConsolePrefs, consoleWarn = app.LoadConsolePrefs(r.settings.ConsolePrefsPath)
	if consoleWarn != "" {
		r.setSettingsNotice("console", consoleWarn, true)
	}
	r.settings.ThemePath = paths.Theme()
	var themeWarn string
	r.settings.ThemePrefs, themeWarn = app.LoadThemePrefs(r.settings.ThemePath)
	if themeWarn != "" {
		r.setSettingsNotice("theme", themeWarn, true)
	}
	r.settings.LocalePrefsPath = paths.Locale()
	var localePrefs app.LocalizationPrefs
	var localeWarn string
	localePrefs, localeWarn = app.LoadLocalePrefs(r.settings.LocalePrefsPath)
	if localeWarn != "" {
		r.setSettingsNotice("locale", localeWarn, true)
	}
	r.settings.LocalePref = localePrefs.Locale
	r.ai.PrefsPath = paths.AI()
	r.gitSync.PrefsPath = paths.Sync()
	r.gitSync.Prefs, r.gitSync.Warn = app.LoadSyncPrefs(r.gitSync.PrefsPath)
	if key, ok := app.LoadSyncKey(r.gitSync.PrefsPath); ok {
		r.gitSync.KeySet = true
		r.gitSync.KeyKCV = sync.KCV(key)
	}
	if vault, err := app.LoadVault(storePath); err != nil {
		r.ai.Warn = locales.RootAiKeyUnavailable(err.Error())
	} else {
		r.ai.Vault = vault
		prefs, warn := app.LoadAIPrefs(r.ai.PrefsPath, vault)
		r.ai.Prefs = prefs
		r.ai.Warn = warn
	}
	r.store = app.LoadStore(storePath)
	r.tree.MetaCache = app.NewMetaCache(paths.MetaDir())
	r.history = app.OpenHistory(paths.HistoryDir())
	if err := r.history.MigrateLegacy(filepath.Join(filepath.Dir(storePath), "history.jsonl")); err != nil {
		app.LogWarn("history", "legacy migration failed", "err", err.Error())
	}
	files := app.ConsoleFiles{Dir: r.store.ConsolesDir(), Store: r.store}
	files.Migrate()
	r.session.Mgr = app.NewConsoleManager(r.store, files)
	r.applyConsolePrefs()
	r.pruneMissingExternal()
	r.restoreSession()
	r.install.Self = app.ReleaseBuild() && !app.UpdateBuild() && runtime.GOOS == "linux"
	if r.install.Self {
		r.install.Need = app.CheckInstall()
	}
	ws, _ := r.session.Mgr.ActiveWorkspace()
	app.LogInfo("startup", "session restored", "consoles", len(r.session.Mgr.Open()), "workspace", ws.Name, "conn_warning", r.store.Warning())
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
	if a.session.Mgr == nil && a.store != nil {
		a.session.Mgr = app.NewConsoleManager(a.store, app.ConsoleFiles{Dir: a.store.ConsolesDir(), Store: a.store})
	}
	return a.session.Mgr
}

// execDoneLogger 返回一次执行结束的日志回调（连接名在执行时读取，避免陈旧）。
func (a *root) execDoneLogger(consoleID string) func(*engine.QueryResult, string, time.Duration) {
	return func(_ *engine.QueryResult, errMsg string, dur time.Duration) {
		// 成功不落日志（语句已在 exec/start 记过）；失败只补错误与耗时。
		if errMsg == "" {
			return
		}
		app.LogError("exec", "failed", "console", consoleID, "dur_ms", dur.Milliseconds(), "err", errMsg)
	}
}

// newConsole 创建一个带当前结果设置（页大小/预算）的运行态。
func (a *root) newConsole() *app.Console {
	c := app.NewConsole()
	c.SetPageSize(a.settings.ResultPrefs.PageSize)
	c.SetResultBudgetMB(a.settings.ResultPrefs.BudgetMB)
	return c
}

// applyResultLimits 把结果设置下发给所有已存在的运行态（含表视图）。
func (a *root) applyResultLimits() {
	set := func(c *app.Console) {
		if c == nil {
			return
		}
		c.SetPageSize(a.settings.ResultPrefs.PageSize)
		c.SetResultBudgetMB(a.settings.ResultPrefs.BudgetMB)
	}
	set(a.con)
	for _, c := range a.tabs.Consoles {
		set(c)
	}
	for _, t := range a.tabs.ViewerTabs {
		if t != nil {
			set(t.rt)
		}
	}
}

// runtime 返回某控制台的执行运行态（懒创建，结果按控制台各自保留）。
func (a *root) runtime(id string) *app.Console {
	if c, ok := a.tabs.Consoles[id]; ok {
		return c
	}
	c := a.newConsole()
	c.SetOnDone(a.execDoneLogger(id))
	if a.win != nil {
		win := a.win
		c.SetPublisher(func(f func()) { a.safeUpdate(win, f) })
	}
	a.tabs.Consoles[id] = c
	return c
}

// pageState 是一个结果 tab 的分页状态。页大小在每次执行时从结果设置取。
type pageState struct {
	Page         int    // 0 基页码
	Size         int    // 每页行数
	Total        int64  // 已统计到的总行数
	TotalKnown   bool   // 是否点过「统计总行数」
	HasMore      bool   // 当前页之后还有行（未知总数时用于启用下一页）
	BaseSQL      string // 原始 SQL，翻页时包成派生表
	Loading      bool   // 正在加载某一页
	CountLoading bool   // 正在统计总行数
	PageRuntime  string // 翻页执行用的运行态 id
	CountRuntime string // 统计执行用的运行态 id
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
	if len(a.tabs.StmtRuns) > 0 && a.tabs.ActiveStmt < len(a.tabs.StmtRuns) {
		return a.tabs.StmtRuns[a.tabs.ActiveStmt].runtimeID
	}
	return a.tabs.ActiveConsole
}

// currentRuntime 返回结果区展示的运行态：多语句时是当前语句，否则是当前控制台。
func (a *root) currentRuntime() *app.Console {
	if len(a.tabs.StmtRuns) > 0 && a.tabs.ActiveStmt < len(a.tabs.StmtRuns) {
		return a.runtimeFor(a.tabs.StmtRuns[a.tabs.ActiveStmt].runtimeID)
	}
	return a.runtimeFor(a.tabs.ActiveConsole)
}

// hasPinned 报告本次执行里是否有钉住的结果 tab。
func (a *root) hasPinned() bool {
	for _, run := range a.tabs.StmtRuns {
		if run.pinned {
			return true
		}
	}
	return false
}

// refreshStmtMarks 按语句范围与状态刷新编辑器 gutter 上的状态圆点：
// 执行中强调色、成功绿色、失败红色。正文不再整行着色，免得和当前行高亮抢注意力。
func (a *root) refreshStmtMarks() {
	st := a.code.state
	st.GutterMarks = nil
	if len(a.tabs.StmtRuns) == 0 || st.Buffer == nil {
		a.code.marks = nil
		st.LineBackgrounds = nil
		return
	}
	buf := st.Buffer
	out := make([]myeditor.GutterMark, 0, len(a.tabs.StmtRuns))
	for _, run := range a.tabs.StmtRuns {
		// 钉住的结果来自旧正文，行号已经失效，不画 gutter 标记。
		if run.pinned || !run.started || run.sql == "" {
			continue
		}
		// 正文被编辑后偏移不再可信：只有当前切片仍等于执行时的语句快照才上色。
		// 这样改过的语句、删掉的行以及被编辑挤位的后续语句都不会再挂错颜色。
		if run.stmt.Start < 0 || run.stmt.Start >= run.stmt.End || run.stmt.End > buf.Len() ||
			buf.Slice(run.stmt.Start, run.stmt.End) != run.sql {
			continue
		}
		first := buf.Pos(run.stmt.Start).Line
		last := buf.Pos(run.stmt.End - 1).Line
		rt := a.runtimeFor(run.runtimeID)
		col := a.stmtOKColor
		switch {
		case rt.Running():
			col = a.stmtRunColor
		case rt.Err() != "":
			col = a.stmtErrColor
		}
		for line := first; line <= last; line++ {
			out = append(out, myeditor.GutterMark{Line: line, Color: col})
		}
	}
	a.code.marks = out
	st.LineBackgrounds = nil
}

// activateConsole 把编辑器切成该控制台的正文，并安排光标回位。
func (a *root) activateConsole(id string) bool {
	m := a.consoleMgr()
	if m == nil {
		return false
	}
	if a.viewerActive() {
		a.leaveViewer()
		if id == a.tabs.ActiveConsole {
			return true
		}
	}
	if id == a.tabs.ActiveConsole {
		return true
	}
	a.ai.Plan.open = false
	prev := a.tabs.ActiveConsole
	if err := a.flushActive(); err != nil {
		return false // 落盘失败不切换，避免丢编辑
	}
	// 旧控制台：flush 之后磁盘是权威，丢掉它的编辑器来源与内存正文。
	m.DetachTextSource(prev)
	rec, ok := m.OpenConsole(id)
	if !ok {
		return false
	}
	if rec.External {
		a.ensureExternal(rec.ID)
	}
	// 目标控制台：同样以磁盘为准（它上次离开时已落盘）。
	m.DetachTextSource(rec.ID)
	a.saveStmtRuns(prev)
	a.loadStmtRuns(rec.ID)
	text, _ := m.Text(rec.ID)
	a.code.SetActive(rec.ID, text)
	start, end := m.Caret(rec.ID)
	a.code.SetSelection(start, end)
	a.tabs.ActiveConsole = rec.ID
	// 状态点来自该控制台的 stmtRuns，必须在 SetActive 换成它的正文之后再算，
	// 否则会沿用上一个控制台的圆点或按错误正文校验而全部丢掉。
	a.refreshStmtMarks()
	a.touchConsoleMRU(rec.ID)
	// 活动控制台的正文惰性取自编辑器：逐帧只比较版本号，落盘时才拷贝。
	m.SetTextSource(rec.ID, func() string { return a.code.Text() })
	a.tabs.LastVer[rec.ID] = a.code.Version()
	a.tabs.LastCaret = [2]int{start, end}
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
	if a.tabs.ActiveConsole != "" {
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

// insertSQLAtCaret 在当前光标处插入一条 SQL，必要时补换行，避免和已有语句粘连。
func (a *root) insertSQLAtCaret(sql string) {
	runes := []rune(a.code.Text())
	start, _ := a.code.CaretRunes()
	if start < 0 || start > len(runes) {
		start = len(runes)
	}
	insert := sql
	if start > 0 && runes[start-1] != '\n' {
		insert = "\n" + insert
	}
	if start < len(runes) && runes[start] != '\n' {
		insert += "\n"
	}
	a.setSelection(start, start)
	a.code.Insert(insert)
	// 光标停在这条 SQL 的末尾，紧接着的执行才会命中它而不是后面的语句。
	end := start + len([]rune(insert))
	if insert[len(insert)-1] == '\n' {
		end--
	}
	a.setSelection(end, end)
}

// setSelection 用 rune 偏移设置选区（测试用；编辑器内部是字节）。
func (a *root) setSelection(startRune, endRune int) { a.code.SetSelection(startRune, endRune) }

// flushActive 落盘当前控制台的正文与光标（切标签/关闭/执行前/退出）。
func (a *root) flushActive() error {
	if a.tabs.ActiveConsole == "" {
		return nil
	}
	m := a.consoleMgr()
	if m == nil {
		return nil
	}
	s, e := a.code.CaretRunes()
	m.SetCaret(a.tabs.ActiveConsole, s, e)
	m.FlushCarets()
	if _, st, ok := a.activeExternal(); ok && (st.conflict || st.missing) {
		// 外部文件冲突/缺失：把编辑器正文存回内存缓存，切标签不丢内容。
		m.SetText(a.tabs.ActiveConsole, a.code.Text())
		return nil
	}
	if err := m.Flush(a.tabs.ActiveConsole); err != nil {
		a.consoleErr = err.Error()
		app.LogWarn("console", "flush failed", "console", a.tabs.ActiveConsole, "err", err.Error())
		return err
	}
	return nil
}

// syncActiveBuffer 把编辑器改动同步给管理器：只在文本版本变化时取 String()（O(n)），
// 只在选区变化时记光标，保持逐帧成本为常数级。
func (a *root) syncActiveBuffer() {
	// 没有激活控制台时的临时缓冲同样要响应编辑：否则旧语句的行底色会留在正文上。
	ver := a.code.Version()
	if a.tabs.ActiveConsole == "" {
		if ver != a.tabs.LastVer[""] {
			a.tabs.LastVer[""] = ver
			a.refreshStmtMarks()
		}
		return
	}
	m := a.consoleMgr()
	if m == nil {
		return
	}
	if ver != a.tabs.LastVer[a.tabs.ActiveConsole] {
		a.tabs.LastVer[a.tabs.ActiveConsole] = ver
		m.TouchVersion(a.tabs.ActiveConsole, ver)
		a.refreshStmtMarks()
	}
	if s, e := a.code.CaretRunes(); [2]int{s, e} != a.tabs.LastCaret {
		a.tabs.LastCaret = [2]int{s, e}
		m.SetCaret(a.tabs.ActiveConsole, s, e)
	}
}

// Attach 在窗口创建后接通主线程回投与原生窗口句柄。
func (a *root) Attach(win *mygo.Window) {
	a.win = win
	win.OnFileDrop(func(e *mygo.FileDropEvent) { a.handleDroppedFiles(e.Paths) })
	win.OnFocus(func() { a.pollExternalFiles(true) })
	win.OnClose(func(e *mygo.CloseEvent) {
		// 导出尚未完成时先确认，避免用户顺手关窗把导出中断成半截文件。
		if a.exportState.Busy {
			e.PreventDefault()
			if !a.exportState.CloseConfirm {
				a.safeUpdate(win, func() { a.exportState.CloseConfirm = true })
			}
			return
		}
		// 外部文件有未解决冲突/缺失且本地有改动时，先让用户选择，别默默丢内容。
		if id := a.firstBlockedExternal(); id != "" {
			e.PreventDefault()
			a.deferActivateConsole(id, "")
			a.external.ResolveOpen = true
			return
		}
		a.FlushAll()
	})
	a.con.SetPublisher(func(f func()) { a.safeUpdate(win, f) })
	for _, c := range a.tabs.Consoles {
		c.SetPublisher(func(f func()) { a.safeUpdate(win, f) })
	}
	// 语言切换后设置窗口的原生标题也要跟着变（标题栏文字每帧重建，不用管）。
	mygo.App.OnLocaleChanged(func(string) {
		if a.settings.Win != nil {
			a.settings.Win.SetTitle(locales.SettingsTitle() + " — " + app.DisplayName)
		}
	})
}

// applyEditorLanguage 让编辑器的 SQL 高亮跟随当前数据源（MySQL 的 AUTO_INCREMENT 等）。
func (a *root) applyEditorLanguage() {
	a.code.SetLanguageKind(a.connKind())
}

// View 是窗口内容入口。
func (a *root) View(c *ui.Context) {
	rootView(a)(c)
}

// recoverPanic 是 UI 线程兜底：panic 只记日志并显示在顶栏，不让应用退出。
func (a *root) recoverPanic(where string) {
	if r := recover(); r != nil {
		app.LogError("panic", where, "err", fmt.Sprint(r), "stack", string(debug.Stack()))
		a.consoleErr = locales.RootInternalError(fmt.Sprint(r))
	}
}

// guardView 包住一帧的构建：panic 时记堆栈并在顶栏显示错误，尽量让应用继续跑。
func (a *root) guardView(fn func()) {
	defer a.recoverPanic("view build")
	fn()
}

// safeUpdate 把后台结果回投 UI 时兜住回调 panic；否则 mygo 主循环会直接退出。
func (a *root) safeUpdate(win *mygo.Window, fn func()) {
	if win == nil {
		return
	}
	a.safeUpdateFunc(win.Update, fn)
}

// safeUpdateFunc 是 safeUpdate 对任意 Update(func()) 窗口接口的版本。
func (a *root) safeUpdateFunc(update func(func()), fn func()) {
	if update == nil {
		return
	}
	update(func() {
		defer a.recoverPanic("async update")
		fn()
	})
}

func rootView(a *root) func(c *ui.Context) {
	return func(c *ui.Context) {
		a.guardView(func() { a.view(c) })
	}
}

// frameStart 是一帧开始时的状态迁移阶段：主题、编辑器缓冲、外部文件轮询，
// 以及上一帧排队的延迟动作。渲染分支此后只读，不再消费 pending* 字段。
func (a *root) frameStart(c *ui.Context) {
	a.applyTheme(c)
	a.flushToasts(c)
	a.maybeOpenSettings()
	a.syncActiveBuffer()
	a.applyEditorLanguage()
	a.pollExternalFiles(false)
	a.drainDeferred()
	a.applyPaletteJump()
	a.dispatchKeys(c)
}

// view 是一帧的真正构建（由 guardView 包裹）。
func (a *root) view(c *ui.Context) {
	defer func() { a.pointerDouble = false }()
	a.frameStart(c)
	{
		a.modals.draw(c)

		pane := func() {
			main := func() {
				ui.Column(c).Fill().Children(func() {
					// 三行：标题栏（命令面板入口）→ 控制台标签 → 数据源与工具。
					a.topBar(c)
					a.tabsRow(c)
					a.connBar(c)
					// 内容区取三行下面剩下的高度：Fill() 会按整窗高度算，把状态栏挤出窗口。
					ui.Column(c).Grow(1).FillWidth().Padding(8).Gap(6).Children(func() {
						if a.viewerActive() {
							a.viewerPane(c)
							a.resultFooter(c)
							return
						}
						// 编辑器 / 结果 分屏：按窗口高度取比例，结果区可 Alt+D 隐藏。
						editor := func() {
							a.externalBanner(c)
							a.code.ViewWithDots(c).Label(locales.TopEditor())
						}
						if a.resultsHidden {
							ui.Column(c).Grow(1).Children(editor)
						} else {
							ui.SplitVerticalPercent(c, &a.editorPercent, editor, func() {
								a.resultPane(c)
							}).Grow(1).SplitReset(65)
						}
						// 状态栏贴在主窗口底部：结果区隐藏、执行计划或结果很高时都不会被挤掉。
						a.resultFooter(c)
					})
				})
			}
			if a.ai.Chat.open {
				if a.ai.Chat.split <= 0 || a.ai.Chat.split >= 100 {
					a.ai.Chat.split = 68
				}
				ui.SplitPercent(c, &a.ai.Chat.split, main, func() {
					a.aiPanel(c)
				}).Fill().SplitReset(68)
				return
			}
			main()
		}
		ui.Box(c).Fill().ObserveInput(a.observeInputMods).Children(func() {
			if a.sidebarHidden {
				pane()
				return
			}
			ui.Split(c, &a.sidebarWidth, func() {
				a.sidebar(c)
			}, pane).Fill()
		})
		// 最后画：浮窗提示盖在内容与弹窗之上。
		a.toastViewport(c)
	}
}

// observeInputMods 记录指针按下/抬起时的修饰键和双击状态，供结果表格
// 的双击手势判定使用。指针事件自带修饰键；不能用键盘累计状态兜底——
// 弹窗打开期间可能收不到修饰键松开事件，累计值会粘住。
func (a *root) observeInputMods(ev ui.InputEvent) {
	switch ev.Kind {
	case ui.InputPointerDown, ui.InputPointerUp:
		a.resultClickMods = ev.Mods
		a.pointerDouble = ev.Clicks >= 2
	}
}
