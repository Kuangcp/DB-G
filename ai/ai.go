package ai

import "context"

// Role 是聊天消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 是一条聊天消息。
type Message struct {
	Role    Role
	Content string
}

// Request 是一次补全请求。
type Request struct {
	Model       string
	Messages    []Message
	Temperature float32
	MaxTokens   int
}

// EventKind 是流式事件类型。
type EventKind int

const (
	EventText EventKind = iota
	EventDone
	EventError
	EventUsage
)

// Usage 是模型返回的 token 用量（部分本地服务不返回）。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Event 是 Provider 流里的一个事件。
type Event struct {
	Kind  EventKind
	Text  string
	Err   error
	Usage *Usage
}

// Provider 是一个聊天模型服务。实现必须支持取消：ctx 结束后关闭事件流。
type Provider interface {
	Models(ctx context.Context) ([]string, error)
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// ConnInfo 是发给模型的连接信息（不含 DSN/密码）。
type ConnInfo struct {
	Kind     string
	Name     string
	Database string
}

// ColumnContext 是一列的可发送元数据。
type ColumnContext struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
}

// TableContext 是一张表的可发送元数据。
type TableContext struct {
	Schema     string
	Name       string
	Comment    string
	Columns    []ColumnContext
	PrimaryKey []string
}

// HistoryItem 是一条可选的查询历史。
type HistoryItem struct {
	SQL  string
	OK   bool
	Err  string
	Rows int
}

// ErrorItem 是一条可选的执行错误。
type ErrorItem struct {
	SQL string
	Err string
}

// TableSample 是一组可选的样本行；第一期不默认发送。
type TableSample struct {
	Table   string
	Columns []string
	Rows    [][]string
}

// ContextSnapshot 是一次请求可用的上下文，由 UI 组装。
type ContextSnapshot struct {
	Connection ConnInfo
	Tables     []TableContext
	ActiveSQL  string
	Selection  string
	History    []HistoryItem
	Errors     []ErrorItem
	Samples    []TableSample
}

// Summary 是给 UI 显示的上下文摘要。
func (s ContextSnapshot) Summary() (tables, columns, rows int) {
	for _, t := range s.Tables {
		tables++
		columns += len(t.Columns)
	}
	for _, sample := range s.Samples {
		rows += len(sample.Rows)
	}
	return tables, columns, rows
}
