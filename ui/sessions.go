package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
	"db-g/locales"
)

// maxOpenSessions 是同时保持连接的数据源个数上限；超过就按 LRU 关掉最久没用的。
const maxOpenSessions = 6

// openTimeout 是一次建连（含 ping）的上限。
const openTimeout = 5 * time.Second

// sessionFor 返回该数据源的会话并刷新它的 LRU 序号；没连上返回 nil。
// 所有按数据源取会话的地方都走这里，不要缓存返回值。
func (a *root) sessionFor(connID string) engine.Session {
	if a.sessions == nil {
		return nil
	}
	sess, ok := a.sessions[connID]
	if !ok {
		return nil
	}
	a.touchSession(connID)
	return sess
}

// connected 报告该数据源当前是否连着（不刷新 LRU：画圆点是只读的）。
func (a *root) connected(connID string) bool {
	if a.sessions == nil {
		return false
	}
	_, ok := a.sessions[connID]
	return ok
}

// connErrOf 返回该数据源上次打开失败的原因（空 = 没有失败记录）。
func (a *root) connErrOf(connID string) string {
	if a.sessionErr == nil {
		return ""
	}
	return a.sessionErr[connID]
}

// touchSession 把该数据源标成「刚用过」。
func (a *root) touchSession(connID string) {
	if a.sessionUse == nil {
		a.sessionUse = map[string]uint64{}
	}
	a.useSeq++
	a.sessionUse[connID] = a.useSeq
}

// activeConnID 返回当前的执行目标数据源：优先激活控制台所属的那个；
// 没有控制台时（临时缓冲执行、脚本导入）退回最近用过的会话。
func (a *root) activeConnID() string {
	if t := a.activeViewerTab(); t != nil {
		return t.connID
	}
	if id := a.activeConsoleConnID(); id != "" {
		return id
	}
	return a.latestSessionID()
}

// latestSessionID 返回最近用过的会话的 id（没有会话时为空）。
func (a *root) latestSessionID() string {
	latest, use := "", uint64(0)
	for id, u := range a.sessionUse {
		if latest == "" || u > use {
			latest, use = id, u
		}
	}
	return latest
}

// activeSession 返回激活控制台所属数据源的会话；没连返回 nil。
func (a *root) activeSession() engine.Session { return a.sessionFor(a.activeConnID()) }

// activeConnName 返回激活控制台所属数据源的展示名（顶栏用）。
func (a *root) activeConnName() string {
	id := a.activeConnID()
	if id == "" || a.store == nil {
		return ""
	}
	if p, ok := a.store.Get(id); ok {
		return p.Name
	}
	return ""
}

// connProbe 是建连时顺带探测到的 schema 信息；由后台协程产出，UI 线程应用。
type connProbe struct {
	active   string   // 会话当前 schema/库
	schemas  []string // 可切换的 schema/库列表
	switched string   // 自动切到的默认 schema（空 = 没切）
}

// dialSession 建立连接并探测 schema。它不碰 root 状态，可以放心跑在后台协程；
// 结果交给 installSession / applyProbe 在 UI 线程落地。
func dialSession(p app.ConnProfile, curSchema string) (engine.Session, connProbe, error) {
	sess, err := engine.Open(engineProfile(p))
	if err != nil {
		return nil, connProbe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		_ = sess.Close()
		return nil, connProbe{}, err
	}
	return sess, probeConn(p, sess, curSchema), nil
}

// probeConn 读取会话的当前 schema 与 schema 列表；连接里没指定库时自动选第一项，
// 避免补全/元数据报「未选择数据库」。语义与原 initSchemaAfterConnect 一致。
func probeConn(p app.ConnProfile, sess engine.Session, curSchema string) connProbe {
	var out connProbe
	sw, ok := sess.(engine.SchemaSwitcher)
	if !ok {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if cur, err := sw.ActiveSchema(ctx); err == nil && cur != "" {
		out.active = cur
	}
	lister, ok := sess.(engine.SchemaLister)
	if !ok {
		return out
	}
	schemas, err := lister.Schemas(ctx)
	if err != nil || len(schemas) == 0 {
		if err != nil {
			app.LogWarn("catalog", "schemas load failed", "conn", p.ID, "err", err.Error())
		}
		return out
	}
	out.schemas = schemas
	if curSchema == "" && out.active == "" {
		target := defaultSchemaFor(p, schemas)
		if err := sw.UseSchema(ctx, target); err != nil {
			app.LogWarn("catalog", "default schema failed", "conn", p.ID, "schema", target, "err", err.Error())
			return out
		}
		out.switched = target
	}
	return out
}

// installSession 把刚建好的会话装进会话表（UI 线程）。
func (a *root) installSession(p app.ConnProfile, sess engine.Session) {
	if a.sessions == nil {
		a.sessions = map[string]engine.Session{}
	}
	a.sessions[p.ID] = sess
	a.touchSession(p.ID)
	delete(a.sessionErr, p.ID)
	a.loadServerInfo(p.ID, sess)
	if a.catalog != nil {
		a.catalog.invalidate(p.ID) // 重连后目录可能变了
	}
	if a.store != nil && p.ID != "" {
		a.store.SetActive(p.ID)
	}
	a.evictSessions(p.ID)
}

// applyProbe 把后台探测到的 schema 信息写回 UI 状态。
func (a *root) applyProbe(p app.ConnProfile, probe connProbe) {
	if probe.active != "" {
		a.tree.ConnSchema[p.ID] = probe.active
	}
	if len(probe.schemas) == 0 {
		return
	}
	if a.tree.Schemas == nil {
		a.tree.Schemas = map[string][]string{}
	}
	a.tree.Schemas[p.ID] = probe.schemas
	if probe.switched != "" {
		a.tree.ConnSchema[p.ID] = probe.switched
	}
	a.saveMetaCache(p)
}

// connect 同步打开一个数据源的会话并装进会话表；已经连着就只刷新 LRU。
// 打开失败会记进 sessionErr（侧栏圆点变红）。UI 路径请用 connectAsync，别在事件回调里直连。
func (a *root) connect(p app.ConnProfile) error {
	if a.sessions == nil {
		a.sessions = map[string]engine.Session{}
	}
	if _, ok := a.sessions[p.ID]; ok {
		a.touchSession(p.ID)
		delete(a.sessionErr, p.ID)
		return nil
	}
	if a.connOpening[p.ID] {
		return errors.New(locales.SessionConnecting())
	}
	a.clearServerInfo(p.ID)
	app.LogInfo("conn", "open", "kind", p.Kind, "name", p.Name, "dsn", app.RedactDSN(p.Path))
	sess, probe, err := dialSession(p, a.tree.ConnSchema[p.ID])
	if err != nil {
		a.setSessionErr(p.ID, err, p)
		return err
	}
	a.installSession(p, sess)
	a.applyProbe(p, probe)
	return nil
}

// connectAsync 在后台建立连接：事件回调里只做状态切换，建连（含探测 schema）
// 全部放到后台，完成后回投 UI 线程安装会话并调用 done。
// 同一数据源重复触发只发一次建连，其余回调排队等这一次的结果。
func (a *root) connectAsync(p app.ConnProfile, done func(error)) {
	if sess, ok := a.sessions[p.ID]; ok && sess != nil {
		a.touchSession(p.ID)
		delete(a.sessionErr, p.ID)
		if done != nil {
			done(nil)
		}
		return
	}
	if a.connWaiters == nil {
		a.connWaiters = map[string][]func(error){}
	}
	if done != nil {
		a.connWaiters[p.ID] = append(a.connWaiters[p.ID], done)
	}
	if a.connOpening == nil {
		a.connOpening = map[string]bool{}
	}
	if a.connOpening[p.ID] {
		return
	}
	a.connOpening[p.ID] = true
	a.clearServerInfo(p.ID)
	app.LogInfo("conn", "open", "kind", p.Kind, "name", p.Name, "dsn", app.RedactDSN(p.Path))
	cur := a.tree.ConnSchema[p.ID]
	var (
		sess  engine.Session
		probe connProbe
		err   error
	)
	a.runBackground(func() {
		sess, probe, err = dialSession(p, cur)
	}, func() {
		a.finishConnect(p, sess, probe, err)
	})
}

// finishConnect 在 UI 线程收尾一次异步建连：安装会话、投递 schema、唤醒等待者。
func (a *root) finishConnect(p app.ConnProfile, sess engine.Session, probe connProbe, err error) {
	delete(a.connOpening, p.ID)
	if err == nil && a.store != nil && p.ID != "" {
		if _, ok := a.store.Get(p.ID); !ok {
			// 建连期间连接被删掉了：不要留下一个没有档案的会话。
			err = errors.New(locales.RunConnDeleted())
			if sess != nil {
				go func() { _ = sess.Close() }()
			}
			sess = nil
		}
	}
	if err == nil && sess != nil {
		if _, ok := a.sessions[p.ID]; ok {
			// 已经有别的路径把它连上了：丢弃这次多出来的会话。
			go func() { _ = sess.Close() }()
			sess = nil
		}
	}
	if err != nil {
		a.setSessionErr(p.ID, err, p)
	} else {
		a.installSession(p, sess)
		a.applyProbe(p, probe)
	}
	waiters := a.connWaiters[p.ID]
	delete(a.connWaiters, p.ID)
	for _, w := range waiters {
		w(err)
	}
}

// connOpeningNow 报告该数据源是否正在后台建连（侧栏画「连接中」）。
func (a *root) connOpeningNow(connID string) bool {
	return a.connOpening[connID]
}

// runBackground 把 work 放到后台执行，完成后把 done 投回 UI 线程。
// headless（无窗口）同步执行，测试才能在调用后立刻断言。
func (a *root) runBackground(work, done func()) {
	if a.bgRun != nil {
		a.bgRun(work, done)
		return
	}
	if a.win == nil {
		work()
		done()
		return
	}
	win := a.win
	go func() {
		work()
		a.safeUpdate(win, done)
	}()
}

// loadServerInfo 异步读取数据源版本与当前数据库；不支持该接口的会话直接跳过。
// headless 测试同步执行，保证断言前结果已经写回；有窗口时用 win.Update 回投。
func (a *root) loadServerInfo(connID string, sess engine.Session) {
	si, ok := sess.(engine.ServerInfoer)
	if !ok {
		if a.serverInfoErr == nil {
			a.serverInfoErr = map[string]string{}
		}
		a.serverInfoErr[connID] = locales.SessionNoVersion()
		return
	}
	if a.serverInfoSeq == nil {
		a.serverInfoSeq = map[string]uint64{}
	}
	a.serverInfoSeq[connID]++
	seq := a.serverInfoSeq[connID]
	apply := func(info engine.ServerInfo, err error) {
		if a.serverInfoSeq[connID] != seq {
			return
		}
		if err != nil {
			if a.serverInfoErr == nil {
				a.serverInfoErr = map[string]string{}
			}
			a.serverInfoErr[connID] = app.ErrorText(err)
			return
		}
		if a.serverInfo == nil {
			a.serverInfo = map[string]engine.ServerInfo{}
		}
		a.serverInfo[connID] = info
		delete(a.serverInfoErr, connID)
	}
	run := func() (engine.ServerInfo, error) {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		return si.ServerInfo(ctx)
	}
	if a.win == nil {
		info, err := run()
		apply(info, err)
		return
	}
	win := a.win
	go func() {
		info, err := run()
		win.Update(func() { apply(info, err) })
	}()
}

// clearServerInfo 丢弃某个数据源的版本信息；会话关闭/重连时调用，
// 同时递增轮次让还在路上的旧结果作废。
func (a *root) clearServerInfo(connID string) {
	if a.serverInfoSeq == nil {
		a.serverInfoSeq = map[string]uint64{}
	}
	a.serverInfoSeq[connID]++
	delete(a.serverInfo, connID)
	delete(a.serverInfoErr, connID)
}

// setSessionErr 记住失败原因，并把它显示给用户看。
func (a *root) setSessionErr(connID string, err error, p app.ConnProfile) {
	if a.sessionErr == nil {
		a.sessionErr = map[string]string{}
	}
	a.sessionErr[connID] = app.ErrorText(err)
	app.LogError("conn", "open failed", "kind", p.Kind, "name", p.Name,
		"dsn", app.RedactDSN(p.Path), "err", err.Error())
	// 顶栏原本固定显示一行错误，但现在打开的可能不是当前控制台的数据源：
	// 用浮窗提示并自动消失，圆点/Tooltip 仍按数据源记着失败原因。
	a.toastError(locales.ConnOpenFailed(p.Name, app.ErrorText(err)))
}

// closeSession 关掉一个数据源的会话（LRU 淘汰、删除连接、退出时用）。
func (a *root) closeSession(connID, reason string) {
	sess, ok := a.sessions[connID]
	if !ok {
		return
	}
	delete(a.sessions, connID)
	delete(a.sessionUse, connID)
	a.clearServerInfo(connID)
	app.LogInfo("conn", "close", "id", connID, "reason", reason)
	if sess != nil {
		if err := sess.Close(); err != nil {
			app.LogWarn("conn", "close failed", "id", connID, "err", err.Error())
		}
	}
}

// cancelForceAfter 是“已请求停止但驱动没回来”的宽限期；测试可临时改小。
var cancelForceAfter = 3 * time.Second

// stopActiveRuns 取消当前控制台里正在跑的语句：先取消 context + 驱动 Cancel；
// 若宽限期内还没结束，强制把运行态作废并摘掉会话，避免 UI 永远停在“执行中”。
func (a *root) stopActiveRuns() {
	connID := a.activeConnID()
	seen := map[*app.Console]bool{}
	add := func(rt *app.Console) {
		if rt == nil || !rt.Running() || seen[rt] {
			return
		}
		seen[rt] = true
		gen := rt.Generation()
		rt.Cancel()
		app.LogInfo("exec", "cancel", "conn", a.activeConnName(), "console", a.tabs.ActiveConsole)
		if a.win != nil {
			win := a.win
			go func() {
				time.Sleep(cancelForceAfter)
				a.safeUpdate(win, func() { a.forceStopRuntime(rt, gen, connID) })
			}()
		}
	}
	for _, run := range a.tabs.StmtRuns {
		add(a.runtimeFor(run.runtimeID))
	}
	for _, ps := range a.tabs.PageStates {
		if ps.PageRuntime != "" {
			add(a.runtimeFor(ps.PageRuntime))
		}
		if ps.CountRuntime != "" {
			add(a.runtimeFor(ps.CountRuntime))
		}
	}
	add(a.currentRuntime())
}

// forceStopRuntime 是停止的兜底：驱动/网络卡死时不再等它，直接把结果作废并摘掉会话。
func (a *root) forceStopRuntime(rt *app.Console, gen uint64, connID string) {
	if rt == nil || !rt.AbortIf(gen, locales.SessionForceStopped()) {
		return
	}
	for _, ps := range a.tabs.PageStates {
		if ps.PageRuntime != "" && a.runtimeFor(ps.PageRuntime) == rt {
			ps.Loading = false
		}
		if ps.CountRuntime != "" && a.runtimeFor(ps.CountRuntime) == rt {
			ps.CountLoading = false
		}
	}
	a.dropSession(connID, "cancel timeout")
	a.consoleErr = locales.SessionStopUnresponsive()
	app.LogWarn("exec", "cancel timeout", "conn", connID)
}

// dropSession 只把会话从内存里摘掉，关闭动作放到后台：
// 驱动/网络卡死时 DB.Close 可能等当前查询，这里不能阻塞 UI。
func (a *root) dropSession(connID, reason string) {
	sess, ok := a.sessions[connID]
	if !ok {
		return
	}
	delete(a.sessions, connID)
	delete(a.sessionUse, connID)
	a.clearServerInfo(connID)
	app.LogWarn("conn", "drop session", "id", connID, "reason", reason)
	if sess != nil {
		go func() { _ = sess.Close() }()
	}
}

// closeAllSessions 关掉全部会话（退出时用）。
func (a *root) closeAllSessions() {
	for id := range a.sessions {
		a.closeSession(id, "shutdown")
	}
}

// evictSessions 在超过上限时按 LRU 关掉最久没用、且没在跑语句的数据源。
func (a *root) evictSessions(keep string) {
	for len(a.sessions) > maxOpenSessions {
		victim := a.lruVictim(keep)
		if victim == "" {
			app.LogWarn("conn", "evict skipped", "open", len(a.sessions))
			return
		}
		a.closeSession(victim, "lru")
	}
}

// lruVictim 找最久没用、不是 keep、也不在跑语句的数据源；找不到返回空。
func (a *root) lruVictim(keep string) string {
	victim, oldest := "", ^uint64(0)
	for id := range a.sessions {
		if id == keep || a.connBusy(id) {
			continue
		}
		use := a.sessionUse[id]
		if victim == "" || use < oldest {
			victim, oldest = id, use
		}
	}
	return victim
}

// connBusy 报告该数据源是否有控制台正在执行语句（有就别淘汰，免得把查询掐掉）。
func (a *root) connBusy(connID string) bool {
	for runtimeID, rt := range a.tabs.Consoles {
		if rt == nil || !rt.Running() {
			continue
		}
		base, _, _ := strings.Cut(runtimeID, "#") // 多语句的运行时是 consoleID#i
		if a.consoleConnID(base) == connID {
			return true
		}
	}
	return false
}

// consoleConnID 返回某控制台绑定的数据源 id。
func (a *root) consoleConnID(consoleID string) string {
	m := a.consoleMgr()
	if m == nil || consoleID == "" {
		return ""
	}
	for _, rec := range m.Open() {
		if rec.ID == consoleID {
			return rec.ConnectionID
		}
	}
	return ""
}

// connDotColor 是数据源状态点的颜色：黄=建连中，红=上次打开失败，绿=连着，灰=没连。
func (a *root) connDotColor(connID string, t *ui.Theme) ui.Color {
	switch {
	case a.connOpeningNow(connID):
		return t.Warning
	case a.connErrOf(connID) != "":
		return t.Danger
	case a.connected(connID):
		return t.Success
	}
	return t.TextMuted
}

// connDot 画数据源状态点；建连中/失败时 Tooltip 给原因。
func (a *root) connDot(c *ui.Context, connID string) {
	dot := ui.Text(c, "●").TextColor(a.connDotColor(connID, c.Theme()))
	if a.connOpeningNow(connID) {
		dot.Tooltip(locales.SessionConnecting())
	} else if msg := a.connErrOf(connID); msg != "" {
		dot.Tooltip(msg)
	}
}

// topBarConn 是第二行的数据源连接开关：只显示一个图标（插头=已连接，
// 拔掉的插头=未连接，红色=上次打开失败），不重复显示数据源名。
// 单击在连接/断开之间切换；断开只关会话并保留已加载的树（下次展开时按 stale 重拉首页）。
func (a *root) topBarConn(c *ui.Context, p app.ConnProfile) {
	t := c.Theme()
	icon, col := iconUnplug, t.TextMuted
	errMsg := a.connErrOf(p.ID)
	opening := a.connOpeningNow(p.ID)
	switch {
	case opening:
		icon, col = iconUnplug, t.Warning
	case errMsg != "":
		icon, col = iconUnplug, t.Danger
	case a.connected(p.ID):
		icon, col = iconPlug, t.Success
	}
	row := ui.ButtonBase(c).Label(locales.SessionDatasource(p.Name)).Gap(4).AlignItems(ui.Center).
		Padding(t.Space(0.5), t.Space(0.75)).Radius(t.Space(1))
	switch {
	case opening:
		row.Tooltip(locales.SessionConnecting())
	case errMsg != "":
		row.Tooltip(errMsg)
	default:
		row.Tooltip(locales.SessionConnectTip())
	}
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		iconChild(c, icon, col)
	})
	if row.Clicked() {
		if a.connected(p.ID) {
			a.disconnectConn(p)
		} else {
			a.expandConnAndTree(p)
		}
	}
}

// disconnectConn 断开用户当前数据源：关会话、失效补全缓存，并把树分组标成过期，
// 重连后展开分组会重新拉第一页/计数；已加载的对象先留着，侧栏不会突然空掉。
func (a *root) disconnectConn(p app.ConnProfile) {
	if !a.connected(p.ID) {
		return
	}
	a.cancelConnRuns(p.ID)
	a.closeSession(p.ID, "user disconnect")
	delete(a.sessionErr, p.ID)
	if a.catalog != nil {
		a.catalog.invalidate(p.ID)
	}
	a.markConnTreeStale(p.ID)
	app.LogInfo("conn", "disconnect", "id", p.ID, "name", p.Name)
}

// cancelConnRuns 取消该数据源上正在执行的语句（断开/关闭连接前调用）。
// 取消只走运行态自己的 context，不再有会话级取消槽。
func (a *root) cancelConnRuns(connID string) {
	for id, rt := range a.tabs.Consoles {
		if rt == nil || !rt.Running() {
			continue
		}
		if a.runtimeConnID(id) == connID {
			rt.Cancel()
		}
	}
	for _, t := range a.tabs.ViewerTabs {
		if t != nil && t.connID == connID && t.rt != nil && t.rt.Running() {
			t.rt.Cancel()
		}
	}
}

// markConnTreeStale 让某连接已加载的树分组在下次展开时替换首页，并丢掉计数。
func (a *root) markConnTreeStale(connID string) {
	if a.tree.GroupStale == nil {
		a.tree.GroupStale = map[string]bool{}
	}
	for base, byKind := range a.tree.Data {
		if !connBaseMatch(connID, base) {
			continue
		}
		for kind := range byKind {
			key := objectGroupKey(base, kind)
			delete(a.tree.PageStarted, key)
			a.tree.GroupStale[key] = true
		}
	}
	for base := range a.tree.Counts {
		if connBaseMatch(connID, base) {
			delete(a.tree.Counts, base)
		}
	}
	for key := range a.tree.Loading {
		if connBaseMatch(connID, key) {
			delete(a.tree.Loading, key)
		}
	}
}
