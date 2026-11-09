# AI 上下文与助手 设计

日期：2026-11-09
状态：已确认方向，按 `plans/2026-11-09-ai-context.md` 实施
关联草稿：`docs/design/AiContext.md`

## 1. 目标与非目标

**目标**

1. Chat-to-SQL：把当前连接、schema、当前 SQL 作为受控上下文，流式生成 SQL，用户确认后插入编辑器。
2. 执行计划可视化：把四种数据源的 EXPLAIN 结果解析成统一计划树；AI 诊断是可选增强。
3. SQL 错误一键修复：SQL 失败后基于错误和涉及表结构生成 diff，用户确认后应用。
4. 运行时可混合：默认本地 Ollama，也可配置任意 OpenAI-compatible 远程服务。
5. 隐私可控：默认只发 schema + 当前 SQL；样本、历史、错误日志逐项开关。

**非目标（第一期）**

- 不让模型自主执行写操作或任意工具调用。
- 不内置模型、不做模型下载。
- 不做向量数据库；先用词法检索 + 外键扩展。
- 不把 AI 放进执行/补全的关键路径，AI 故障不影响原有功能。

## 2. 已确认决策

| 决策 | 结果 |
|---|---|
| 运行模式 | 混合：本地 Ollama 默认 + OpenAI-compatible 远程 |
| SDK | `github.com/sashabaranov/go-openai`，不自己写 SSE |
| 首批能力 | Chat-to-SQL、EXPLAIN 可视化 + AI 诊断、SQL 错误一键修复 |
| 默认出网 | schema + 当前 SQL；采样/历史/日志默认关闭 |
| EXPLAIN ANALYZE | 每次确认，且只允许 SELECT |
| 高危 SQL | 确定性规则默认提醒，连接级可开硬阻断 |
| 会话 UI | 右侧可折叠面板 |
| 会话持久化 | 按工作区 `ai/conversations/<workspaceID>.jsonl` |
| 硬阻断配置 | 连接档案字段，不放 AI 设置 |

## 3. 分层

```text
ui/ai_*.go      会话面板、流式渲染、上下文组装、插入/取消
app/ai.go       ai.json、Provider 配置、密钥（复用 Vault）、出网策略
app/guard.go    高危 SQL 规则（确定性）
ai/             Provider、SDK 封装、上下文 DTO、提示词、检索、计划解析
engine/         可选能力：SchemaIntel / TableSampler / PlanExplainer
```

依赖方向保持 `ui → app → ai → 标准库`，`ui → engine`；`ai` 不 import ui/app/engine。

## 4. 配置与密钥

`<config>/db-g/ai.json`：

```json
{
  "baseURL": "http://127.0.0.1:11434/v1",
  "model": "qwen2.5-coder:7b",
  "apiKey": "enc:v1:...",
  "allowSampleRows": false,
  "allowHistory": false,
  "allowErrorLogs": false
}
```

- API Key 复用 `app.Vault`（`secret.key`，0600）加密；内存中才解密。
- 默认地址指向 Ollama 的 OpenAI-compatible `/v1`。
- 日志只记录 provider/model/耗时/token，不记录 prompt、key、DSN。

## 5. 上下文

```go
type ContextSnapshot struct {
    Connection ConnInfo
    Tables     []TableContext
    ActiveSQL  string
    Selection  string
    History    []HistoryItem
    Errors     []ErrorItem
    Samples    []TableSample
}
```

组装规则：

1. 当前连接：kind、database/schema；不发 host/password/DSN。
2. 表：来自现有 catalog；列只带缓存中已有的，未加载的只带表名。
3. 当前 SQL：`app.StatementAt` 取光标所在语句；有选区时附选区。
4. 相关度排序：问题词法命中表名/列名/注释 → 外键一跳 → 当前 SQL 引用 → 历史频次。
5. token 预算：system 10% / schema 40% / SQL 20% / 历史样本 20% / 输出预留 10%。
6. UI 显示上下文摘要与完整预览，实际发送内容必须与预览一致。

## 6. Provider

```go
type Provider interface {
    Models(ctx context.Context) ([]string, error)
    Stream(ctx context.Context, req Request) (<-chan Event, error)
}

type Event struct {
    Kind  EventKind // text | done | error
    Text  string
    Err   error
    Usage *Usage
}
```

- `ai/openai.go` 用 go-openai SDK 的 `CreateChatCompletionStream`。
- 默认 OpenAI-compatible 覆盖 OpenAI/DeepSeek/Ollama/vLLM/LM Studio。
- 每个请求带 context timeout；Esc/停止/关面板取消。
- UI 流式增量按约 30–60ms 合并后回主线程；旧请求用 generation id 丢弃。

## 7. Chat-to-SQL

- 右侧面板：模型、上下文摘要、消息列表（Markdown 流式）、输入框（Enter 发送，Shift+Enter 换行）。
- 生成结果先展示，不自动执行；“插入 SQL”从 ```` ```sql ```` 代码块提取。
- 会话按工作区 JSONL 落盘，只存消息、模型、时间、上下文统计；不存样本行。

## 8. EXPLAIN

- `engine.PlanExplainer` 可选接口，新增 `CapExplain`。
- 解析器把 PG JSON、MySQL JSON/TREE、ClickHouse JSON、SQLite `EXPLAIN QUERY PLAN` 统一成：

```go
type PlanNode struct {
    Op, Relation string
    Cost, Rows   float64
    Actual       time.Duration
    Detail       string
    Children     []PlanNode
}
```

- 结果区新增「执行计划」Tab；解析失败回退原始结果。
- `EXPLAIN ANALYZE` 每次确认，且执行前用确定性分析确认是 SELECT。
- AI 诊断输入计划树 + 索引/外键，不改变基础可视化。

## 9. 错误修复与高危拦截

- 错误修复：保存最近失败 SQL + 驱动错误 + 涉及表结构；模型返回受控 diff；用户点“应用”才改编辑器。
- 高危规则在 `app/guard.go`：无 WHERE 的 UPDATE/DELETE、恒真条件、DROP/TRUNCATE、估算影响行数超阈值；默认提醒，连接档案可设硬阻断。
- AI 只解释风险，不参与放行。

## 10. 测试

- `ai`：SDK 流式解析用 `httptest` 假服务；取消、错误、空响应。
- `ai` 上下文：排序、预算、脱敏、SQL 代码块提取。
- `app`：`ai.json` 加载/保存/密钥加密。
- `ui`：注入 fake Provider，验证发送、流式状态、停止、插入 SQL、设置保存。
- EXPLAIN/guard 在对应阶段补 fixture 与规则表测试。

## 11. 阶段

1. 基础设施 + Chat-to-SQL（本分支第一阶段）。
2. EXPLAIN 可视化 + AI 诊断。
3. SQL 错误修复。
4. 高危拦截与连接级策略。
5. 内联 AI 补全。
