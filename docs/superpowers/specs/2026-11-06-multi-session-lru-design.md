# 多数据源会话（最多 6 个，LRU）设计

日期：2026-11-06
状态：已由用户拍板（2026-11-06 对话确认两点：启动不连/不重载；淘汰后不显示「未连接」）

## 问题

`ui/root.go` 里只有一个 `sess` 字段，`setSession()` 打开新连接时会 `Close()` 掉旧的。
所以连上 MySQL 再去连 ClickHouse，MySQL 的会话被关掉，它展开的对象树下一帧会写出
「未连接」，用户没法同时用两个数据源。

侧栏的圆点还有个反向问题：启动时 `checkConnHealth()` 会给所有数据源做健康检查，能连上的
一律标绿，看起来像「已经连上了」，其实一个都没连。

## 目标

1. 允许多个数据源同时连接，默认上限 6 个，超过按 LRU 关掉最久没用的。
2. 启动时**不连任何数据源、不重载对象树**；只有用户点某个数据源时才连它。
3. 圆点表达真实状态：灰 = 没有会话，绿 = 会话在，红 = 上次打开失败（Tooltip 显示原因）。
4. 树里不再出现「未连接」这行字。

## 非目标

- 不保留启动时的批量健康检查（与「启动不连」直接矛盾）：`probeConn` / `checkConnHealth`
  整体删除，`connStatus` 状态表随之去掉。
- 不做「连接池」：一个数据源一个 `engine.Session`，与现在一致。
- 不改执行模型：会话仍是各自驱动里的 `*sql.DB`，多会话并发执行互不影响。

## 设计

### 状态

```go
const maxOpenSessions = 6

sessions   map[string]engine.Session // connID → 会话
sessionUse map[string]uint64         // connID → 最近一次使用的序号（LRU）
sessionErr map[string]string         // connID → 上次打开失败的原因（红点 + Tooltip）
useSeq     uint64
```

删除 `sess` / `sessConnID` / `failedConnID` / `activeConnID` / `connStatus` / `connName` / `connErr`。

### 统一入口

```go
sessionFor(connID string) engine.Session   // 取会话并刷新 LRU；没有就 nil
connected(connID string) bool
connErrOf(connID string) string            // 只读，不刷新 LRU
activeSession() engine.Session             // = sessionFor(激活控制台的数据源)
connect(p app.ConnProfile) error           // 开连接（已连则只 touch）
closeSession(connID, reason string)        // 关闭并从表里删掉
```

- 执行、对象树、DDL、补全、编辑计划、取更多、导出全部改成按 connID 取会话，
  不再读「当前唯一会话」。
- 控制台相关的路径（执行/结果区/可编辑/导出）用 `activeSession()`，因为它天然属于激活控制台。

### LRU 淘汰

`connect()` 成功后如果 `len(sessions) > maxOpenSessions`，从最久没用的开始淘汰：

- 跳过刚连上的这个（`keep`）。
- 跳过「正在跑语句」的数据源：遍历运行态，凡是 runtimeID 属于该数据源控制台的、且 `Running()`
  为真，就跳过（避免把查询中途掐掉）。
- 都忙就临时超限，记 `app.LogWarn("conn", "evict skipped", …)`，下次连接再淘汰。
- 淘汰即 `Close()` 会话 + 删表项；展开过的树**保留上次内容**（不清），点变灰，下次执行自动重连。

### 连接时机（只有一处来源）

- 启动：`newRootWithStore` 不调任何连接/探测；树不预载。
- 用户点数据源行（名称）→ 连上并展开；点展开箭头也连。
- 执行时 `ensureConsoleConn()` 兜底重连控制台所属数据源。
- 「刷新」按钮只 `reloadTree()`（清缓存），不主动连任何数据源。

### 圆点

`connDotColor(connID)`：`sessionErr` 非空 → 红；`connected` → 绿；否则灰。
红点时 Tooltip 显示错误文本。树里的「未连接」错误行删除（`loadGroupsIn` 不再 setTreeErr）。

## 影响面

- `ui/{root,run,objecttree,completion,result,editable,sidebar}.go` 共 ~45 处 `a.sess`/`sessConnID` 引用。
- 删除 `ui/health.go` 及其测试；`Readme.md` 里健康检查那条改成新的圆点语义。
- 测试里大量 `a.sess.RunStatement(...)` 播种数据 → 换成 `sessionFor(connID)`（单连接场景用一个
  `testSession(t)` 助手）。

## 验收

- 连 A、再连 B：A 的会话仍在，A 展开的树不再出现「未连接」，两个数据源都能执行。
- 连第 7 个：最久没用的那个被关掉（点变灰），其余 6 个可用；正在跑查询的不会被关。
- 启动后所有点为灰；点某个数据源才变绿；连不上时变红且 Tooltip 给原因。
- `go build ./... && go vet ./... && go test -p 1 ./...` 全绿。
