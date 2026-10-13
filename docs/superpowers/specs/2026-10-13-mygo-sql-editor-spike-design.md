# mygo SQL 编辑器可行性基准（Spike）设计

日期：2026-10-13
状态：已批准（用户在方案过审后指示「开始做吧」，并指定 go1.27.1）

## 背景

db-k（Compose Desktop 数据库客户端，`/home/zk/Work/ai/db-k`）的 SQL 编辑器在多行/大 SQL 编辑时卡顿，结构根因已有实测文档（`EDITOR_HIGHLIGHT.md`）：
- `BasicTextField` 每次按键对整篇文档重排版，无段落虚拟化；
- 高亮排版 O(段数 × span 数)；
- 应用层又整篇重扫高亮 + 整篇重测 textLayout + O(n) 补全扫描。

mygo（Go 原生桌面框架，自绘 GPU 渲染）提供了分块索引文本树 `TextBuffer` 与段落级虚拟化的 `TextAreaBuffer`。本 spike 用一个最小窗口基准应用验证「mygo 能否根治此卡顿」，结论用于决定是否把 db-k 迁移到 mygo，或反哺 Kotlin 版的修复思路。

## 验证目标

对比 db-k 实测同量级场景，量出三档规模的「单键延迟」与「滚动帧时间」：

| 规模 | 对应 db-k 场景 | 预估 span 数 |
|---|---|---|
| 3k 行 / ~224k 字符 | 拖入大 SQL 卡死场景 | ~22k |
| 12k 行 | 高亮预算关闭前 7.7s/帧场景 | ~92k |
| 100k 行 | mygo 官方示例量级 | ~700k |

核心假设：
1. 段落虚拟化使「整篇重排版」消失，按键成本趋近 O(可见段落)（框架源码 `textarea.go place()` 已确认只布局视口段，布局按 spans 缓存）。
2. `rangeSpans` 对每个可见段落遍历全部 ranges（O(可见段 × 总 span)），且 `ed.ranges` 每帧清空重建并要求传入整份排序 ranges——因此在 92k+ span 下，「全量 ranges」策略有真实成本，而「视口 ranges」策略（R ~350）可将每帧成本降两个数量级。两者必须对比实测。
3. 语法高亮必须由应用层增量维护（只重扫脏行及其影响区），mygo 不提供；若照搬 db-k 的整篇重扫仍会卡。

## 方案：窗口基准应用

### 位置与模块

项目根：`/home/zk/Code/go/db-k`（Go module `db-k`，go 1.27.1，依赖 `github.com/egoist/mygo`）。

```
main.go            窗口 + 布局 + HUD（组装各模块）
highlight/
  token.go         增量行级 SQL 扫描器：维护 per-line token cache，只重扫脏行 + 影响区
  ranges.go        span 合并 → []ui.TextRange；输出「全量 full」与「视口 viewport」两种策略
editor.go          TextAreaBuffer 挂载、注入 ranges、TrackLines 行号槽、当前行高亮
findbar.go         查找栏 + 匹配高亮
metrics.go         单键延迟采样 + HUD 显示 + CSV 落盘
fixtures/          生成的 3k/12k/100k 行 .sql
bench_test.go      ui.Tester headless 采样（CI 可复现数字）
```

### 数据流

```
按键 → TextBuffer 变更 → 标记脏行 → 增量重扫(μs) → 合成 ranges（全量 or 视口）
     → ui.TextRanges 注入 → 虚拟化帧渲染 → 采样「按键→帧绘制完成」延迟
```

插桩点：按键事件时刻 → 下一帧绘制/布局完成的时刻，测量真实输入延迟（不只 tokenize 耗时）。

### 度量与成功判定门槛

指标：单键中值 / 90 / 99 分位延迟、滚动帧时间、ranges 数量、rangeSpans 每帧成本。

门槛：
- 3k 行：单键中值 < 16ms；
- 12k 行：中值 < 16ms；若超则验证视口策略能否救回；
- 100k 行：仅记录数据，不设硬门槛。

结论三种：A. 可行（全过）；B. 需视口优化才可行（正结果）；C. 不可行（12k 无策略可救 → 数据为否决证据）。

### 关键 API 依赖（v0.4.8）

- `ui.NewTextBuffer(string)` → `*ui.TextBuffer`（分块索引树）
- `ui.TextAreaBuffer(c, b)` + `.TrackLines(*ui.TextLines)`（行号）、`.TrackScroll`（滚动）
- `.TextRanges(ranges ...ui.TextRange)`（注入高亮；每帧重置）
- `ui.Tester`（headless 采样）
- 行号槽：`Draw` + `TextLines.Top/At`（官方文档示例模式）

### 风险与对策

| 风险 | 对策 |
|---|---|
| 本机 go 1.26.5，mygo 要求 1.27.1 | 已定位 `/home/zk/Code/go/bin/go1.27.1`，全程用它构建（已验证 `go mod init` + 依赖下载成功，go.mod = go 1.27.1） |
| Linux 原生 UI 依赖 GTK+OpenGL，运行环境缺库 | 回退 CPU raster 后端；headless `ui.Tester` 数字仍有效 |
| IME 组合期计时波动 | 采样时剔除组合期按键 |
| mygo v0.4.x 个人维护框架风险 | 本次仅 spike 验证性能，不构成长期技术选型承诺 |

### 交付物

- 可运行的窗口基准应用（含 HUD）；
- `bench_test.go` 可复现数字；
- 测量报告（双策略 × 三规模的延迟矩阵）；
- 结论：可行 / 需视口优化 / 不可行，及对 db-k 后续的启示。