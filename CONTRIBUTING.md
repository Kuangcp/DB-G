# Contributing to DB-G

感谢关注 DB-G。本文档面向想为项目代码做贡献的开发者；日常用户指引见 [Readme.md](Readme.md)。

## 快速开始

```bash
# 运行交互窗口（CGO_ENABLED=0，本机 cgo 链接有 .sframe 问题）
make run

# 编译 + 全量测试 + vet
make test && make vet

# Linux x64 发布构建
make build

# Windows / Linux 双平台发布
make release          # dist/ 下产出两个静态二进制
```

需要 Go 1.27（`go1.27` 在 PATH 上），其余依赖由 `go.mod` 管理。无需 CGO、无需 Node/Rust 工具链。

## 代码结构

import 方向是硬约束，下层不能依赖上层：

```
main.go ──> ui ──> app ──> engine
        └──> driver/<kind> ──> engine
```

| 包 | 职责 |
|---|---|
| `engine/` | 协议无关契约：Session / Capabilities / Profile / QueryResult |
| `driver/<kind>/` | SQLite / MySQL / PostgreSQL / ClickHouse 实现；`init()` 里 `engine.Register` |
| `app/` | 纯逻辑与落盘：连接库、控制台、历史、日志、加解密、导出、SQL 切分 |
| `ui/` | mygo 视图层（唯一 import mygo 的地方） |
| `tools/` | 生成器（图标等），不参与运行时 |

新增数据源详见 [AGENTS.md](AGENTS.md)「新增一个数据源」章节，必须遵守 5 步流程。

## 开发约定

- **语言**：界面文案一律中文，不做 i18n。
- **颜色**：UI 代码一律从 `c.Theme()` 取色（`Text` / `Surface` / `Accent` …），禁止硬编码。数据源品牌色除外。
- **图标**：使用 Lucide 描线 SVG。通过 `node tools/lucideicons/main.mjs` 重新生成 `ui/icons_gen.go`，不手写 SVG 路径。
- **按钮**：单图标按钮用 `iconButtonPlain`（无底悬停显色），文字按钮保持可见。成排操作放 `Toolbar(c, func(){…})`。
- **弹窗**：内容随选择变化的弹窗定尺寸（`Modal(...).Width(w).Height(h)`），避免抖动。
- **线程**：长活（连库、执行 SQL、读 DDL、导出）离开 UI 线程，结果用 `win.Update` 回投。所有长活加 `context.WithTimeout`。
- **持久化**：配置落盘位置见 [AGENTS.md](AGENTS.md)「持久化分层」。新增状态字段只要可 JSON 序列化即可，无迁移框架。
- **日志**：用 `app.LogInfo/LogWarn/LogError(tag, msg, "k", v, …)`，tag 已有：startup conn exec console catalog export import write ddl install external panic。密码 / DSN 经 `app.RedactDSN()` 脱敏后再记。

完整的样式规范、会话管理、快捷键系统、设置窗口写法见 [AGENTS.md](AGENTS.md)，这是本项目的"圣经"，改之前必读。

## 提交测试

每个 commit 必须过以下检查：

```bash
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -p 1 ./...
go tool mygo vet .
gofmt -l .        # 输出必须为空
```

可简写为 `make test && make vet`。

### 测试约定

- 交互 / 布局 / 状态机优先 headless 测试（`ui.NewTester(rootView(a), 1200, 700)`），不弹真窗口。
- 测试**绝不碰真实配置**：用 `t.TempDir()` 指到临时目录。
- 修 bug 先写失败的测试，再改代码。
- 性能基准跑 `./ui -run 'TestEditor.*Perf'`，注意 `-p 1` 并行度。
- `TestAllConnKindsLinked` 会检查 main 是否把所有 driver 的 `_` import 补上——新增 kind 后记得同步更新此测试列表。

## Git 工作流

- commit message 中文，前缀 `feat:` / `fix:` / `ui:` / `docs:` / `chore:`，一次只做一件事。
- 新功能走完 brainstorm → design → plan 流程，文档放在 `docs/superpowers/specs/` 和 `docs/superpowers/plans/`。
- 合入前：PR 描述列明验证步骤；功能变更同步更新 Readme.md 勾选清单和 TODO.md。

## 如何提贡献

1. Fork 仓库，开分支 `feature/<name>` 或 `fix/<name>`。
2. 改代码 + 写测试 + 过 `make test && make vet`。
3. Push → 发 PR，附复现步骤（bug fix）或截图（ui 改动）。

## FAQ

**Q: 能加新的数据源吗？**
A: 可以。先在 `engine/session.go` / `engine/caps.go` 确认现有接口够用，然后按 [AGENTS.md](AGENTS.md)「新增一个数据源」5 步走。如果新驱动需要 engine 暴露新接口，先改 engine（纯叶层），再补驱动和 UI。

**Q: 能做 AppImage / deb 打包吗？**
A: 不需要。分发物就是 `go build` 产出的静态二进制，打包不在规划内。

**Q: 我的改动要跨多个包？**
A: 先确认 import 方向合法（下层不动上层）。改了导出的类型签名（`engine/` / `app/`）时，顺带把上游调用方都补齐，不要留断掉的编译。
