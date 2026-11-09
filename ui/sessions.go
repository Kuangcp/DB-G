package ui

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/app"
	"db-g/engine"
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

// connect 打开一个数据源的会话并装进会话表；已经连着就只刷新 LRU。
// 打开失败会记进 sessionErr（侧栏圆点变红）。
func (a *root) connect(p app.ConnProfile) error {
	if a.sessions == nil {
		a.sessions = map[string]engine.Session{}
	}
	if _, ok := a.sessions[p.ID]; ok {
		a.touchSession(p.ID)
		delete(a.sessionErr, p.ID)
		return nil
	}
	app.LogInfo("conn", "open", "kind", p.Kind, "name", p.Name, "dsn", app.RedactDSN(p.Path))
	sess, err := engine.Open(engineProfile(p))
	if err != nil {
		a.setSessionErr(p.ID, err, p)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if err := sess.Open(ctx); err != nil {
		a.setSessionErr(p.ID, err, p)
		return err
	}
	a.sessions[p.ID] = sess
	a.touchSession(p.ID)
	delete(a.sessionErr, p.ID)
	a.initSchemaAfterConnect(p, sess)
	if a.catalog != nil {
		a.catalog.invalidate(p.ID) // 重连后目录可能变了
	}
	if a.store != nil && p.ID != "" {
		a.store.SetActive(p.ID)
	}
	a.evictSessions(p.ID)
	return nil
}

// setSessionErr 记住失败原因，并把它显示给用户看。
func (a *root) setSessionErr(connID string, err error, p app.ConnProfile) {
	if a.sessionErr == nil {
		a.sessionErr = map[string]string{}
	}
	a.sessionErr[connID] = err.Error()
	app.LogError("conn", "open failed", "kind", p.Kind, "name", p.Name,
		"dsn", app.RedactDSN(p.Path), "err", err.Error())
	a.connErr = err.Error()
}

// closeSession 关掉一个数据源的会话（LRU 淘汰、删除连接、退出时用）。
func (a *root) closeSession(connID, reason string) {
	sess, ok := a.sessions[connID]
	if !ok {
		return
	}
	delete(a.sessions, connID)
	delete(a.sessionUse, connID)
	app.LogInfo("conn", "close", "id", connID, "reason", reason)
	if sess != nil {
		if err := sess.Close(); err != nil {
			app.LogWarn("conn", "close failed", "id", connID, "err", err.Error())
		}
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
	for runtimeID, rt := range a.consoles {
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

// connDotColor 是数据源状态点的颜色：红=上次打开失败，绿=连着，灰=没连。
func (a *root) connDotColor(connID string, t *ui.Theme) ui.Color {
	switch {
	case a.connErrOf(connID) != "":
		return t.Danger
	case a.connected(connID):
		return t.Success
	}
	return t.TextMuted
}

// connDot 画数据源状态点；失败时 Tooltip 给原因。
func (a *root) connDot(c *ui.Context, connID string) {
	dot := ui.Text(c, "●").TextColor(a.connDotColor(connID, c.Theme()))
	if msg := a.connErrOf(connID); msg != "" {
		dot.Tooltip(msg)
	}
}

// topBarConn 是顶栏数据源名 + 状态点：双击在连接/断开之间切换。
// 断开只关会话并保留已加载的树（下次展开时按 stale 重拉首页）。
func (a *root) topBarConn(c *ui.Context, p app.ConnProfile) {
	t := c.Theme()
	row := ui.ButtonBase(c).Label("数据源 "+p.Name).Gap(4).AlignItems(ui.Center).
		Padding(t.Space(0.5), t.Space(0.75)).Radius(t.Space(1)).
		Tooltip("双击连接 / 断开数据源")
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		a.connDot(c, p.ID)
		name := ui.Text(c, p.Name).Bold().SingleLine()
		if !a.connected(p.ID) {
			name.TextColor(t.TextMuted)
		}
	})
	if row.DoubleClicked() {
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
	if sess := a.sessionFor(p.ID); sess != nil {
		sess.Cancel()
	}
	a.closeSession(p.ID, "user disconnect")
	delete(a.sessionErr, p.ID)
	a.connErr = ""
	if a.catalog != nil {
		a.catalog.invalidate(p.ID)
	}
	a.markConnTreeStale(p.ID)
	app.LogInfo("conn", "disconnect", "id", p.ID, "name", p.Name)
}

// markConnTreeStale 让某连接已加载的树分组在下次展开时替换首页，并丢掉计数。
func (a *root) markConnTreeStale(connID string) {
	if a.treeGroupStale == nil {
		a.treeGroupStale = map[string]bool{}
	}
	for base, byKind := range a.treeData {
		if !connBaseMatch(connID, base) {
			continue
		}
		for kind := range byKind {
			key := objectGroupKey(base, kind)
			delete(a.treePageStarted, key)
			a.treeGroupStale[key] = true
		}
	}
	for base := range a.treeCounts {
		if connBaseMatch(connID, base) {
			delete(a.treeCounts, base)
		}
	}
	for key := range a.treeLoading {
		if connBaseMatch(connID, key) {
			delete(a.treeLoading, key)
		}
	}
}
