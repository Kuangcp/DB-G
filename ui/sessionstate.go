package ui

import (
	"db-g/app"
	"db-g/engine"
)

// sessionState 是数据源会话池、LRU、建连中状态与服务器信息。
type sessionState struct {
	Mgr     *app.ConsoleManager
	Catalog *catalogCache

	Sessions map[string]engine.Session // connID → 会话（最多 maxOpenSessions 个）
	Use      map[string]uint64         // connID → 最近使用序号（LRU）
	Err      map[string]string         // connID → 上次打开失败的原因
	Opening  map[string]bool           // connID → 后台建连中
	Waiters  map[string][]func(error)  // connID → 等这次建连结果的回调（UI 线程执行）
	BgRun    func(work, done func())   // 后台执行 + 回投 UI 的注入点（测试用）
	Seq      uint64

	ServerInfo    map[string]engine.ServerInfo // connID → 数据库名/版本
	ServerInfoErr map[string]string            // connID → 版本查询失败原因
	ServerInfoSeq map[string]uint64            // connID → 异步查询轮次
}
