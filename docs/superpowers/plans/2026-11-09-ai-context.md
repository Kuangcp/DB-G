# AI 上下文与助手 实施计划

设计：`docs/superpowers/specs/2026-11-09-ai-context-design.md`

## 阶段 1：基础设施 + Chat-to-SQL

### 1. ai 包

- `ai/ai.go`：`Provider`、`Request`、`Message`、`Event`、`ContextSnapshot`、`TableContext`。
- `ai/openai.go`：go-openai SDK 封装，`CreateChatCompletionStream` → `<-chan Event`。
- `ai/prompt.go`：系统提示词、上下文渲染、token 估算、```` ```sql ```` 提取。
- `ai/retrieve.go`：按问题词法给表排序，保留当前 SQL 引用的表。
- 测试：`httptest` 假 OpenAI SSE、取消、错误、SQL 提取、排序。

### 2. app 配置

- `app/ai.go`：`AIPrefs`、默认路径、`LoadAIPrefs`/`SaveAIPrefs`（Vault 加密 API Key）。
- 测试：默认值、加密落盘、往返。

### 3. engine / catalog 支撑

- `ui/catalog.go` 暴露只读的 `objects()` / `columns()`（不改变缓存语义）。
- 本阶段不新增 engine 接口；EXPLAIN/SchemaIntel 留到阶段 2。

### 4. UI

- `ui/ai_chat.go`：状态、上下文快照、发送/取消、事件合并、插入 SQL。
- `ui/ai_panel.go`：右侧可折叠面板、Markdown 流式消息、TextArea 输入。
- `ui/settings.go`：新增「AI」分区（baseURL、model、API key、三个出网开关）。
- `ui/root.go`：加载 `ai.json` / Vault；顶栏 AI 图标；主区域右侧面板。
- `ui/icons_gen.go`：生成 sparkles / send / circle-stop 图标。
- 测试：fake Provider 发送/流式/停止/插入；设置持久化。

### 5. 文档

- `Readme.md` / `README.en.md`：AI 面板与配置说明。
- `TODO.md`：删除已完成的 AI 草稿项，保留后续阶段。

## 验收

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test -p 1 ./...
go tool mygo vet .
gofmt -l .
```

- 默认不发送历史/样本/错误。
- Ollama/远程连接失败只显示面板错误，不影响查询。
- 停止/关面板能取消流。
- 生成的 SQL 必须用户点击才插入。
