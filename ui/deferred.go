package ui

// deferUI 把一个动作排到下一帧帧首执行。视图构建中途不能换编辑器 buffer、
// 不能安全弹窗/切标签，这类意图统一进队列，而不是各自加一个 pending* 字段。
func (a *root) deferUI(fn func()) {
	if fn == nil {
		return
	}
	a.deferred = append(a.deferred, fn)
}

// drainDeferred 按 FIFO 执行队列；执行期间新增的动作也会在同一轮排空。
func (a *root) drainDeferred() {
	for len(a.deferred) > 0 {
		fn := a.deferred[0]
		a.deferred = a.deferred[1:]
		fn()
	}
}

// deferActivateConsole 安排下一帧激活控制台；appendSQL 非空时随后插入正文。
func (a *root) deferActivateConsole(id, appendSQL string) {
	if id == "" && appendSQL == "" {
		return
	}
	a.deferUI(func() {
		if id != "" {
			a.activateConsole(id)
		}
		if appendSQL != "" {
			a.insertSQLAtCaret(appendSQL)
		}
	})
}

// deferActivateViewer 安排下一帧激活表视图标签。
func (a *root) deferActivateViewer(id string) {
	if id == "" {
		return
	}
	a.deferUI(func() { a.activateViewer(id) })
}

// deferCloseConsole 安排下一帧关闭控制台。
func (a *root) deferCloseConsole(id string) {
	if id == "" {
		return
	}
	a.deferUI(func() { a.closeConsole(id) })
}

// deferSwitchWorkspace 安排下一帧切工作区，并激活该工作区上次的控制台。
func (a *root) deferSwitchWorkspace(id string) {
	if id == "" {
		return
	}
	a.deferUI(func() {
		m := a.consoleMgr()
		if m == nil {
			return
		}
		if _, ok := m.SetActiveWorkspace(id); !ok {
			return
		}
		if rec, ok := m.Active(); ok {
			a.activateConsole(rec.ID)
		} else {
			a.clearEditorConsole()
		}
	})
}
