package ui

import (
	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
)

// gridSnap 是一块结果网格的界面状态快照。控制台结果区与表视图数据页共用同一套
// 网格/编辑代码；只有一块网格可见，所以切换标签时把这些状态在 root 与各自快照之间搬一次。
type gridSnap struct {
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

// newGridSnap 返回一块空网格的状态（首次打开表视图时用）。
func newGridSnap() gridSnap {
	return gridSnap{view: newResultView()}
}

// stashGrid 把 root 上的网格状态收进快照（切换标签前调用）。
func (a *root) stashGrid() gridSnap {
	snap := gridSnap{
		state: a.resultState, sel: a.resultSel, row: a.resultRow,
		view: a.resView, last: a.lastResult,
		widthsFor: a.widthsFor, widths: a.widths,
		editKey: a.editKey, plan: a.editPlan, readonly: a.readonlyReason,
	}
	if snap.view == nil {
		snap.view = newResultView()
	}
	return snap
}

// loadGrid 把快照铺回 root（切换标签后调用）。
func (a *root) loadGrid(snap gridSnap) {
	a.resultState = snap.state
	a.resultSel = snap.sel
	a.resultRow = snap.row
	a.resView = snap.view
	if a.resView == nil {
		a.resView = newResultView()
	}
	a.lastResult = snap.last
	a.widthsFor, a.widths = snap.widthsFor, snap.widths
	a.editKey, a.editPlan, a.readonlyReason = snap.editKey, snap.plan, snap.readonly
	a.stopInlineEdit()
	a.editCellOpen, a.previewOpen, a.filterOpen = false, false, false
}

// viewerActive 报告当前展示的是否是表视图标签。
func (a *root) viewerActive() bool {
	return a.activeViewer != "" && a.viewerTabs[a.activeViewer] != nil
}

// activeViewerTab 返回当前展示的表视图标签；没有返回 nil。
func (a *root) activeViewerTab() *viewerTab {
	if !a.viewerActive() {
		return nil
	}
	return a.viewerTabs[a.activeViewer]
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
	return a.lastExecSQL
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
