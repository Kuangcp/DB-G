# mygo SQL 编辑器可行性基准 — 结果报告

日期：2026-10-13
分支：`main` @ `db-k`（`/home/zk/Code/go/db-k`）
结论：**B — 需视口（viewport）策略才可行（正结果）**

## 环境

| 项 | 值 |
|---|---|
| OS | Linux 6.12.25-1-MANJARO x86_64 |
| CPU | Intel Core i5-10400F @ 2.90GHz（12 逻辑核） |
| 内存 | 31 GiB |
| Go | go1.27.1 linux/amd64 |
| mygo | github.com/egoist/mygo v0.4.8 |
| 测量方式 | `ui.Tester` headless（CPU raster，无窗口），`TestSpikeMatrix` |

数据采集：`-count=2`，两轮数字一致（见 `/tmp/opencode/matrix-final.txt` 与 `/tmp/opencode/matrix-*.csv`）。

## 配置说明

三配置 = 两处作用域的笛卡尔子集：

- **tokenize**：`full`（每次按键整篇重扫）或 `viewport`（只扫视口窗口 ± margin=4 行）。
- **inject**：`full`（向 `TextRanges` 注入全文所有 ranges）或 `viewport`（只注入视口窗口内的 ranges）。

| 标签 | tokenize | inject | 语义 |
|---|---|---|---|
| FF | full | full | 照搬 db-k 的「整篇重扫 + 整篇注入」 |
| FV | full | viewport | 只救注入侧 |
| VV | viewport | viewport | 理想：只处理可见部分 |

`injected` = 该帧交给 `TextRanges` 的 range 数。

## 矩阵（代表性一轮；单键 24 样本，滚动/帧各 12 样本）

单键 = 在编辑器聚焦后 `Type("a")`（含一次完整布局+绘制）的墙钟延迟。

| 规模 | 配置 | injected | 单键中值 | 单键 p90 | 单键 p99 | 滚动中值 | 帧中值 |
|---|---|---|---|---|---|---|---|
| 3k | FF | 23 250 | **18.9 ms** | 20.6 ms | 22.4 ms | 8.8 ms | 8.9 ms |
| 3k | FV | 217 | 7.1 ms | 8.7 ms | 11.0 ms | 1.2 ms | 1.5 ms |
| 3k | VV | 217 | **2.5 ms** | 3.0 ms | 3.9 ms | 1.2 ms | 1.2 ms |
| 12k | FF | 93 000 | **69.1 ms** | 76.8 ms | 84.5 ms | 30.3 ms | 28.6 ms |
| 12k | FV | 217 | 21.8 ms | 23.3 ms | 24.0 ms | 1.4 ms | 1.6 ms |
| 12k | VV | 217 | **5.7 ms** | 6.3 ms | 7.4 ms | 1.4 ms | 1.4 ms |
| 100k | FF | 775 000 | **644.8 ms** | 678.4 ms | 6.6 s | 327 ms | 328 ms |
| 100k | FV | 217 | 167.0 ms | 183.4 ms | 206.7 ms | 1.2 ms | 1.1 ms |
| 100k | VV | 217 | **31.4 ms** | 35.9 ms | 40.1 ms | 1.1 ms | 1.2 ms |

门槛（spec）：3k 单键中值 < 16ms；12k 中值 < 16ms（超则看视口能否救回）；100k 仅记录。

## 判定

- 3k×FF = 18.9ms > 16ms → 连 3k 都压不住「整篇注入」，**A（全量也够）不成立**。
- 12k×FF = 69.1ms ≫ 16ms，但 12k×VV = 5.7ms < 16ms → 视口策略救回 → **结论 B**。
- 100k 记录：VV 31.4ms（超 16ms，但见下方分解；无硬门槛）。12k×VV 远低于门槛，排除结论 C。

## 成本分解（由 `injected` 与各配置增速推断）

1. **虚拟化本身成立**：FV/VV 的滚动与稳态帧时间在 3k→100k 几乎恒定（1.1–1.6ms），证明 `TextAreaBuffer` 只布局视口段、不做整篇重排版。FF 的帧/滚动随规模线性膨胀（8.9→28.6→328ms），正是「整篇 ranges 注入」下 `rangeSpans` 的 O(可见段 × 总 range) 成本。
2. **整篇注入是主杀手**：100k 时 FF 注入 775k ranges，单键 644.8ms（p99 6.6s，GC 尖峰）；改成视口注入（FV）后同规模注入降到 217，单键降到 167ms（约 3.9×）。注入侧的收益在 12k 更明显（69.1→21.8ms）。
3. **整篇 tokenize 是次杀手**：FV（仅视口注入、仍整篇 tokenize）与 VV 的差值即 tokenize/文档复制的 O(n) 成本——12k 为 21.8→5.7ms，100k 为 167→31.4ms。两者都随规模近似线性，说明瓶颈是每次按键的 `Buf.String()` 全文复制 + `LineStarts` 全文扫描，而不是词法本身。
4. **残余 O(n) 在 100k×VV**：31.4ms 中绝大部分是每键复制全文（100k 行、数 MB）与重建行起点表。真正的增量方案（只取脏行、只更新脏行起点）可把这一项压到 O(脏行)，这也是后续若选 mygo 的必做优化。

## Fixtures 简化说明

对标 SQL 仅覆盖**行内结构**：`'...'` 字符串（含 `''` 转义）、`--` 行注释、数字、关键字。**不支持跨行块注释 `/* */` 与多行字符串**。这足以产生 db-k 同量级的 span 密度，但不构成完整 SQL 词法；结论中的「tokenize 成本」应理解为该简化词法的成本下界。

## 对 db-k 的启示

1. **段落级虚拟化能根治「整篇重排版」**：这是 Kotlin `BasicTextField` 结构性缺失的部分。mygo 的稳态帧/滚动时间与文档规模无关（~1.2ms），而 db-k 现状是「大 SQL 拖入即卡」。
2. **视口注入是必须的逃生通道**：框架每帧清空并重排 `TextRanges`，且 `rangeSpans` 对每个可见段遍历全部 ranges。桌面/大文档场景必须把注入的 range 限制在视口窗口内（本例注入从 775k 降到 217，单键 644ms→31ms）。这是 db-k 用 Compose 无法绕开的同一类约束，反哺 Kotlin 版时可作为「只给可见区高亮」的整改方向。
3. **编辑器智能与数据层仍需自研**：mygo 不提供语法高亮、补全、诊断，也没有 JDBC/Redis/ES 协议层。迁移到 mygo 等于重写编辑器智能层与全部数据驱动；收益是拿到原生虚拟化文本域与 GPU 渲染底座。若迁移，需配套增量行缓存以消除残余 O(n) 全文复制。

## 交付物

- 可运行窗口基准应用（HUD/toolbar/查找栏）：`main.go` + `editor.go` + `session`（`highlight/`、`metrics.go`、`findmatch.go`）。
- 可复现 headless 矩阵：`bench_test.go`（`TestSpikeMatrix`）。
- 原始样本：`/tmp/opencode/matrix-{3000,12000,100000}-{FF,FV,VV}.csv`。
- 复跑命令：`/home/zk/Code/go/bin/go1.27.1 test ./... -run TestSpikeMatrix -v -count=2`。

## 复现 / 运行

```bash
# headless 基准
/home/zk/Code/go/bin/go1.27.1 test ./... -run TestSpikeMatrix -v

# 交互窗口（需本机 GTK+OpenGL；SCOPE=FF|FV|VV，默认 VV）
/home/zk/Code/go/bin/go1.27.1 run .
```