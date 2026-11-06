# db-g 开发约定（AGENTS.md）

本文件对后续所有 agent/开发者生效。与 `Readme.md`、`docs/superpowers/specs/*` 冲突时以本文件为准：
**功能清单看 `Readme.md`，接下来做什么看 `docs/superpowers/specs/2026-11-05-db-g-roadmap-v2-design.md`，
怎么写看这里。**

db-g 是 Go + [mygo](https://github.com/egoist/mygo) 的数据库客户端，对标 db-k（Kotlin + Compose Desktop）。
界面文案一律中文；不做 i18n。

## Agent harness规约
- 如果需要使用到 subagent 需要有reviewer 以及多个需要切换模型来调用的场景下，请继续使用当前使用的模型，不要自己做选择去换模型，否则可能用到很贵的模型 消耗很多钱。

## 工具链（不要改动）

- Go 1.27：`go1.27` 在 PATH 上（`go version` = go1.27.3），Makefile 用的就是它。
- **CGO 必须关**：本机 cgo 链接有 `.sframe` 问题，`CGO_ENABLED=0` 是唯一可靠组合。
- mygo 工具已由 `go.mod` 的 `tool github.com/egoist/mygo/cmd/mygo` 提供，用 `go tool mygo …` 调用。

```bash
make run      # 交互窗口（CGO_ENABLED=0 go1.27 run .）
make build    # CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go1.27 build → ./db-g
make install  # go1.27 install
make test     # CGO_ENABLED=0 go1.27 test -p 1 ./...（-p 1：别让并行包抢 CPU 把基准跑超限）
make vet      # go vet + go tool mygo vet .（UI 生命周期检查，无输出即通过）

CGO_ENABLED=0 go test ./ui -run 'TestEditor.*Perf' -v   # 只跑编辑器性能基准
```

**打包：不做。** `go build` 出来的静态二进制就是分发包，不做 AppImage / Deb / 安装器 / 自动更新
（roadmap v2 的决策表已拍板）。不要把打包流程写进 Makefile 或 CI。

## 分层（import 方向是硬约束）

```
main.go ──> ui ──> app ──> engine
        └──> _ driver/<kind> ──> engine
```

| 包 | 职责 | 允许依赖 |
|---|---|---|
| `engine/` | **协议无关契约**：`Session` / `Capabilities` / `Profile` / `QueryResult` / `Params` | 只用标准库 |
| `driver/<kind>/` | 各数据源实现（`sqlite` `mysql` `postgres` `clickhouse`），`init()` 里 `engine.Register` | `engine` + 驱动库，**禁止 import app/ui** |
| `app/` | 纯逻辑与落盘：连接库、控制台文件、历史、日志、加解密、导出、SQL 切分/格式化/编辑计划 | `engine`，**禁止 import ui / mygo** |
| `ui/` | mygo 视图层（唯一 import mygo 的地方）：`root` 编排一切交互 | `app`、`engine`、`mygo` |
| `tools/` | 生成器（`genicon` 应用图标、`lucideicons` 图标代码），不是运行时代码 | 标准库 / mygo |

`engine` 是纯叶层：新增能力先加常量或可选接口，绝不为某个驱动开洞。

### 新增一个数据源（照单走，别漏）

1. `driver/<kind>/`：实现 `engine.Session`（`Open`/`Close`/`RunStatement`/`Cancel`/`Caps`），
   可选接口按能力实现（`ColumnLister`/`ObjectLister`/`SchemaLister`/`ObjectDDLer`/`Transactional`/`RowStreamer`）；
   `init()` 里 `engine.Register("<kind>", openSession)`。
2. **`main.go` 加 `_ "db-g/driver/<kind>"`** —— 漏了不会编译报错，只会在打开连接时报
   `engine: unknown data source kind`（历史事故：postgres / clickhouse 都漏过）。
3. `main_test.go` 的 `TestAllConnKindsLinked` 列表补上这个 kind（它会替你把第 2 步守住）。
4. UI 侧：`ui/sidebar.go` 的 `connKinds`（类型列表）、`connKindIcon`/`connKindColor`（`ui/dbicons.go`）、
   `defaultPort`、`paramsPlaceholder`。
5. `Readme.md` 功能清单勾上。

## 会话契约与能力位

UI 靠 `Capabilities` 位降级功能，不靠类型断言猜：

| 位 | 打开什么 |
|---|---|
| `CapEditableResult` | 结果可编辑（还要会话实现 `Transactional`，两者都满足才放开） |
| `CapFetchMore` | 「取更多」（依赖可重放的 SELECT，派生表分页） |
| `CapObjectDDL` | 右键「查看 DDL」 |
| `CapObjects` / `CapColumns` | 对象树 / 列目录与补全 |
| `CapSQLCompletion` | 参与 SQL 补全 |

连接参数：`app.ConnProfile` → `ui.engineProfile()` → `engine.Profile`，**三处打开连接
（`connect` / 测试连接）都走这个函数**，不要各自拼 `engine.Profile`。
`Protocol`（ClickHouse native/http/https）与 `Params`（`key=value`，见 `engine.Params`）在这里带过去。

## 会话管理（★ 必读）

- `sessions map[connID]engine.Session`：一个数据源一个会话，**同时最多 6 个**（`maxOpenSessions`），
  超过按 LRU 关掉最久没用的；正在跑语句的跳过（全忙就临时超限并记 warn）。
- 取会话**一律用 `sessionFor(connID)`**（顺带刷新 LRU），不要缓存会话指针；
  控制台相关操作（执行/结果/可编辑/导出）用 `activeSession()`，它天然属于激活控制台。
- **启动不连任何数据源、不预载对象树**。连接时机只有两处：点数据源名（`expandConnAndTree`）、
  执行时 `ensureConsoleConn()` 兜底。不要在任何初始化路径里偷偷建连接。
- 圆点颜色由 `connDotColor()` 决定：红 = 上次打开失败（`sessionErr`，Tooltip 给原因）、
  绿 = 有会话、灰 = 没连。没连上时**不要往树里写「未连接」这类错误行**，圆点已经说明状态。

## 线程与并发（★ 必读）

- **构建 UI 只在主线程**：`root.view()` 每帧重建界面；所有状态字段读写都在这一条线程上。
- 长活（执行 SQL、连库、读 DDL、导出、测试连接）必须离开 UI 线程，结果用 `win.Update(func(){…})` 回投。
  统一写法：

  ```go
  run := func() (string, bool) { /* 后台做的事 */ }
  if a.win == nil {          // headless（测试）：同步执行，测试才能断言
      done(run())
      return
  }
  win := a.win
  go func() { msg, ok := run(); win.Update(func() { done(msg, ok) }) }()
  ```

- 任何可能卡住的操作都要 `context.WithTimeout`：打开连接 5s（`openTimeout`）、测试连接 5s、DDL 5s。
- `rootView` 用 `guardView` 兜 panic：构建期 panic 记日志 + 顶栏显示错误，应用继续跑。
- 不要在回调里跨线程读 `root` 字段（用闭包捕获值），否则数据竞争。

## 持久化分层（新增要落盘的状态先看这里）

位置一律在 `os.UserConfigDir()/db-g/`（`app.DefaultStorePath()` 等给出）：

| 内容 | 位置 | 说明 |
|---|---|---|
| 连接档案 / 工作区 / 控制台元数据 / 活动项 | `connections.json` | `ConnStore`；密码 **AES-256-GCM** 加密（密钥 `secret.key`，0600），启动时把存量明文自动加密回写 |
| 控制台正文 | `consoles/<id>.sql` | 一个控制台一个文件；切标签/执行前/退出前落盘，平时防抖 |
| SQL 历史 | `history.jsonl` | 5MB 轮转 |
| 日志 | `logs/app-YYYYMMDD.log` | JSON Lines，一天一文件，5MB 轮转，0600 |
| 编辑器显示设置（字号/字体族） | `editor.json` | `app.EditorPrefs`，改动即时生效并落盘 |
| 快捷键改绑 | `keymap.json` | `app.SaveKeymap`，只存与默认不同的动作 |
| 纯瞬态 | 内存 | 结果集、分页状态、UI 开关、补全缓存（TTL 5 分钟） |

判断口诀：**删掉实体要不要一起消失、要不要按它查询？** 要 → `connections.json` 的结构化字段；
不要 → 独立文件或内存。加字段只要 JSON 可序列化即可（无迁移框架，旧文件缺字段按零值处理）。

## 快捷键（★ 必读）

**一个动作表管全部键位**，不要再往各处散写 `c.Shortcut(...)`：

- `ui/keymap.go`：`keyActions`（动作、默认键位、作用域、编辑器命令）+ `"Ctrl+Enter"` 这类
  字符串的解析/格式化。加动作 = 在表里加一行（`ID` 是持久化键名，别随便改）。
- 作用域决定"什么时候才注册"：`scopeAlways`（编辑器聚焦也生效）、`scopeEditor`、
  `scopeNoEditor`（结果区）。**必须先判作用域再调 `c.Shortcut`** ——
  `c.Shortcut` 一旦返回 true 就把键消费掉，判断放在后面等于让别的控件收不到键
  （历史 bug：结果区的 Ctrl+C 吃掉了编辑器的复制）。
- 编辑区的动作复用 mygo 的 `editor.Command`（`DuplicateLinesDown` / `DeleteLine` …），
  由 `applyEditorBindings()` 灌进 `State.Bindings`；做不了的动作交给窗口，绑成
  `NoCommand` 让编辑器别吞键。`scopeNoEditor` 的动作不碰编辑器绑定。
- 改键：`setChords/resetChords` 会**存盘 + 重建编辑器绑定**；同作用域撞键时自动把
  原来的动作解绑（`breakConflicts`）。设置窗口「快捷键」一节点「录制」后按新组合即可。
- 持久化：`<config>/db-g/keymap.json`，只存与默认值不同的动作（`app/keymap.go`）。
  测试要改键就把 `a.keymapPath` 指到 `t.TempDir()`，**不要写用户的真实配置**。

## 设置窗口（★ 必读）

左右布局：左侧分区导航 + 右侧内容 + 左下角版本号（`app.Version()`，`db-g -version` 也能看）。

- 分区在 `ui/settings.go` 的 `settingsSections()`：编辑器 / 快捷键 / 连接档案 / 日志 / 开发。
  新增一个分区 = 加一行 `{标签, 构建函数}`，右侧内容自动可滚。
- 导航行用 `ui/navrow.go` 的 `sideNavRow()`：无按钮外观、行底 1px 分隔线、选中 accent 浅底 +
  加粗。连接弹窗左侧的数据源列表用的是同一个（`connKindRow`），新增侧边列表照这个来。
- 编辑器设置（字号 8–24 / 字体族）改完立即 `applyEditorPrefs()`：更新 `code.state.Font`
  并写 `editor.json`；预览框用同一份数据渲染，别另算。

## UI 约定（★ 必读）

参照 mygo 的 `docs/ui/*.md`（`/home/zk/Code/go/mygo/docs/ui/`），本项目的固定做法：

### 颜色与主题

1. **颜色一律取 `c.Theme()`**：`Text` / `TextMuted` / `Surface` / `SurfaceHover` / `Border` / `Accent` /
   `Danger` / `Success` / `Background`。**禁止硬编码颜色**（浅色深色都得能看）。
2. 唯一的例外是**数据源品牌色**：`connKindColor()` 的 PG 蓝、MySQL 青、SQLite 蓝、CK 暗金，
   只用于类型图标。
3. 想弱化就 `TextColor(c.Theme().TextMuted)`，不要自己调 alpha 近似。

### 图标

4. 图标一律 **Lucide 描线**（24×24、`fill="none" stroke="currentColor" stroke-width="2"`、圆头）。
   `ui/icons_gen.go` 由 `node tools/lucideicons/main.mjs` 生成，**不要手写 SVG 路径**，
   更不要用 `fill="currentColor"`（描线图会被填成一坨实心色块）。
5. 加图标：在 `tools/lucideicons/main.mjs` 的 `icons` 表加一行 `iconXxx: "lucide-name"`，重跑生成器。
   图标在 16px 下要能认出来，选之前先确认小尺寸不糊。

### 按钮

6. **只显示图标的按钮**用 `ui/iconbutton.go` 的两个助手：
   - `iconButton(c, icon, "中文名", color)`：`Button` 有底；`Label`（读屏）+ `Tooltip`（含义）必填。
   - `iconButtonPlain(...)`：无底，悬停才浮浅底；用于列表行、标签关闭这类不必常驻面孔的地方。
7. 成排的操作放进 `ui.Toolbar(c, func(){…}).Label("…")`：工具条里的按钮**悬停前没有面孔**，
   这是这个项目"去掉大方块"的解法；Spacer 把后面的推到右边。
8. 弹窗里的「取消 / 保存 / 删除」保持**文字按钮**（用户已聚焦在那个上下文，文字更快）。
   破坏性操作用 `c.Theme().Danger` 上色。

### 弹窗

9. 内容会随选择变化的弹窗（如新建连接：切数据源类型后表单行数不同）必须**定尺寸**：
   `ui.Modal(c, &open, func(){…}).Width(w).Height(h)` + 内容里用 `Spacer` 留白，
   否则窗口会跟着内容一弹一弹。当前连接弹窗是 `640×420`（`connFormWidth/Height`）。
10. 弹窗内的提示（成功/失败/已复制）用**单行** `SingleLine()` + `Tooltip` 看全文，
    避免长错误把按钮挤出面板。

### 布局

11. 侧栏/列表里的可选项用**行**（`ButtonBase` + `Justify(ui.Start)` + 底边 1px 分隔线 +
    选中 `Accent.Alpha(0.12)` + 加粗），不要用一堆按钮堆出"大方块"。
12. 空态与只读原因要**显示给人看**（"请先连接数据源"、"只读：JOIN 结果不支持写操作"），
    不要静默禁用。

## 验证方式（★ 必读）

**能做程序化 UI 验证，就别开真窗口。** mygo 自带 headless tester：

```go
tt := ui.NewTester(rootView(a), 1200, 700)   // a 是 *root（测试里用 newConsoleRoot/newSidebarRoot）
tt.Frame()
tt.HasText("执行 (Ctrl+Enter)")               // 文案是否存在
tt.Click("保存")                              // 按 Label/文案点击（会校验命中目标）
tt.Find("保存")                               // 拿 Rect 量布局
tt.Texts() / tt.Clipboard() / tt.Image()      // 文案表 / 剪贴板 / 像素
```

- 交互、布局、剪贴板、状态机一律先写 headless 测试；`ui/*_test.go` 里有大量可抄的例子。
- 测试**绝不碰真实配置**：`newConsoleRoot(t)` / `newSidebarRoot(t)` 把连接库指到 `t.TempDir()`，
  需要体检特定档案时再 `newRootWithStore(path)` 注入。
- `tt.Image()` 可以取像素做断言（本项目用它审过图标形状与高亮色，但**别拿它当"截图给人看"**）。
- 拖拽手感、深色下的主观观感、像素级美观这类判断不了的事：**写清步骤让用户验收**，
  不要自己开 GUI 实例、不要动用户真实的数据目录。
- 每次改完必须过：

  ```bash
  CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -p 1 ./...
  go tool mygo vet .
  gofmt -l .        # 必须为空
  ```

- 修 bug 先写**失败的测试**再改（不写测试的修复不算修完）。

## 日志与脱敏

- 用 `app.LogInfo/LogWarn/LogError(tag, msg, "k", v, …)`，tag 已有：`startup` `conn` `exec`
  `console` `catalog` `export` `import` `write` `ddl` `panic`；新增行为先想"出事怎么查"。
- **密码与 DSN 必须脱敏**：`app.RedactDSN()`；日志里永远不出现 `Password`。
- 日志写入失败不能影响调用方（日志不能把应用搞崩）。

## 提交与文档

- commit message 用中文，前缀 `feat:` / `fix:` / `ui:` / `docs:` / `chore:`；一次提交只做一件事。
- 做完一个阶段同步：`Readme.md` 功能清单勾选、`TODO.md` 里划掉已完成项。
- 设计/计划写在 `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` 与
  `docs/superpowers/plans/…`（流程见 superpowers 技能，别跳过 brainstorm → design → plan）。
