package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// gridState 是一块结果网格的界面状态。控制台与每个表视图标签各自持有一份，
// 切换标签只换 root.grid 指针，不再逐字段 stash/load。
type gridState struct {
	state     ui.ListState
	sel       ui.Selection[int]
	row       int
	view      *resultView
	last      *engine.QueryResult
	widthsFor *engine.QueryResult
	widths    []float32
	editKey   string
	plan      *app.EditPlan
	readonly  string
}

// newGridState 返回一块空网格的状态。
func newGridState() *gridState {
	return &gridState{view: newResultView()}
}

// viewerActive 报告当前展示的是否是表视图标签。
func (a *root) viewerActive() bool {
	return a.tabs.ActiveViewer != "" && a.tabs.ViewerTabs[a.tabs.ActiveViewer] != nil
}

// activeViewerTab 返回当前展示的表视图标签；没有返回 nil。
func (a *root) activeViewerTab() *viewerTab {
	if !a.viewerActive() {
		return nil
	}
	return a.tabs.ViewerTabs[a.tabs.ActiveViewer]
}

// gridRuntime 返回网格展示结果对应的运行态：表视图用它的临时运行态。
func (a *root) gridRuntime() *app.Console {
	if t := a.activeViewerTab(); t != nil {
		if t.rt == nil {
			t.rt = a.newConsole()
		}
		return t.rt
	}
	return a.currentRuntime()
}

// gridRuntimeID 返回网格状态的桶键：控制台用运行态 id，表视图用 "vw:"+标签 id。
func (a *root) gridRuntimeID() string {
	if t := a.activeViewerTab(); t != nil {
		return viewerRuntimeID(t.id)
	}
	return a.currentRuntimeID()
}

// gridSession 返回网格数据所在数据源的会话。
func (a *root) gridSession() engine.Session {
	if t := a.activeViewerTab(); t != nil {
		return a.sessionFor(t.connID)
	}
	return a.activeSession()
}

// gridSQL 返回生成当前结果的 SQL（编辑计划、复制 INSERT 用）。
func (a *root) gridSQL() string {
	if t := a.activeViewerTab(); t != nil {
		return t.query.sql
	}
	return a.tabs.LastExecSQL
}

// gridKind 返回网格数据源的种类（引用符/格式化用）。
func (a *root) gridKind() string {
	if t := a.activeViewerTab(); t != nil {
		if k := a.connKindOf(t.connID); k != "" {
			return k
		}
		return "sqlite"
	}
	return a.connKind()
}

// gridConnID 返回网格数据源的连接 id。
func (a *root) gridConnID() string {
	if t := a.activeViewerTab(); t != nil {
		return t.connID
	}
	return a.activeConsoleConnID()
}

// gridErr 在网格上方提示错误：控制台写顶栏，表视图弹浮窗（顶栏属于控制台）。
func (a *root) gridErr(msg string) {
	if a.viewerActive() {
		if msg != "" {
			a.toastError(msg)
		}
		return
	}
	a.consoleErr = msg
}
