package ai

import (
	"strings"
	"unicode/utf8"
)

// DefaultContextWindow 是未配置时的模型上下文窗口（token）。保守取值，
// 本地 Ollama 常见默认在 8k 上下，避免一次请求直接超限。
const DefaultContextWindow = 8192

// DefaultOutputReserve 是未指定输出上限时给回答预留的 token。
const DefaultOutputReserve = 1200

// Budget 是一次提示词的 token 预算。ContextWindow 为 0 时用默认值，
// MaxOutput 为 0 时按调用类型给默认值。
type Budget struct {
	ContextWindow int
	MaxOutput     int
}

// Limit 返回输入侧可用 token 上限：窗口减输出预留，再留一点协议余量。
func (b Budget) Limit() int {
	win := b.ContextWindow
	if win <= 0 {
		win = DefaultContextWindow
	}
	out := b.MaxOutput
	if out <= 0 {
		out = DefaultOutputReserve
	}
	limit := win - out - 512
	if limit < 512 {
		limit = 512
	}
	return limit
}

// PromptReport 是一次裁剪后的上下文报告，供 UI 显示“实际发了什么、丢了什么”。
type PromptReport struct {
	Limit         int // 输入 token 上限
	Tokens        int // 实际提示词估算 token
	Tables        int // 最终包含的表
	Columns       int // 最终包含的列
	Dialogue      int // 最终包含的对话消息
	DDL           int // 最终包含 DDL 的表数
	ResultRows    int // 结果集当前页行数（未带结果时为 0）
	ResultColumns int
	HasPlan       bool

	DroppedTables   int
	DroppedColumns  int
	DroppedDialogue int
	DroppedDDL      int
	DroppedHistory  int
	DroppedErrors   int
	DroppedSamples  int

	TruncatedSQL       bool
	TruncatedSelection bool
	TruncatedExtra     bool
	TruncatedPlan      bool
	TruncatedText      bool // 补全场景的前/后文被截断，或最后兜底截断
}

// Trimmed 报告这次是否发生过任何裁剪。
func (r PromptReport) Trimmed() bool {
	return r.DroppedTables > 0 || r.DroppedColumns > 0 || r.DroppedDialogue > 0 ||
		r.DroppedDDL > 0 || r.DroppedHistory > 0 || r.DroppedErrors > 0 ||
		r.DroppedSamples > 0 || r.TruncatedSQL || r.TruncatedSelection ||
		r.TruncatedExtra || r.TruncatedPlan || r.TruncatedText
}

// tokenTotal 估算整组消息的 token。
func tokenTotal(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content)
	}
	return total
}

// trimSchemaStep 按优先级做一步裁剪：样本 → 历史 → 错误 → 低排名表的列 →
// 超出排名的表 → 其余列 → 只保留最相关的表。返回 false 表示再无可裁。
func trimSchemaStep(s *ContextSnapshot, rep *PromptReport) bool {
	if s.Result != nil && len(s.Result.Sample) > 0 {
		rep.DroppedSamples += len(s.Result.Sample)
		s.Result.Sample = nil
		return true
	}
	if len(s.Samples) > 0 {
		rep.DroppedSamples += len(s.Samples)
		s.Samples = nil
		return true
	}
	if len(s.History) > 1 {
		keep := len(s.History) / 2
		rep.DroppedHistory += len(s.History) - keep
		s.History = s.History[:keep]
		return true
	}
	if len(s.History) == 1 {
		rep.DroppedHistory++
		s.History = nil
		return true
	}
	if len(s.Errors) > 1 {
		keep := len(s.Errors) / 2
		rep.DroppedErrors += len(s.Errors) - keep
		s.Errors = s.Errors[:keep]
		return true
	}
	if len(s.Errors) == 1 {
		rep.DroppedErrors++
		s.Errors = nil
		return true
	}
	// 对话从最旧的消息开始丢，保留最近的上下文。
	if len(s.Dialogue) > 1 {
		keep := len(s.Dialogue) / 2
		rep.DroppedDialogue += len(s.Dialogue) - keep
		s.Dialogue = s.Dialogue[len(s.Dialogue)-keep:]
		return true
	}
	if len(s.Dialogue) == 1 {
		rep.DroppedDialogue++
		s.Dialogue = nil
		return true
	}
	for i := range s.Tables {
		if s.Tables[i].DDL != "" {
			rep.DroppedDDL++
			s.Tables[i].DDL = ""
			return true
		}
	}
	// 排名靠后的表先只留表名；通常前 8 张足够回答。
	for i := len(s.Tables) - 1; i >= contextKeepColumnsTables; i-- {
		if len(s.Tables[i].Columns) > 0 {
			rep.DroppedColumns += len(s.Tables[i].Columns)
			s.Tables[i].Columns = nil
			s.Tables[i].PrimaryKey = nil
			return true
		}
	}
	if len(s.Tables) > contextKeepColumnsTables {
		rep.DroppedTables += len(s.Tables) - contextKeepColumnsTables
		s.Tables = s.Tables[:contextKeepColumnsTables]
		return true
	}
	for i := len(s.Tables) - 1; i >= 0; i-- {
		if len(s.Tables[i].Columns) > 0 {
			rep.DroppedColumns += len(s.Tables[i].Columns)
			s.Tables[i].Columns = nil
			s.Tables[i].PrimaryKey = nil
			return true
		}
	}
	if len(s.Tables) > 1 {
		rep.DroppedTables++
		s.Tables = s.Tables[:len(s.Tables)-1]
		return true
	}
	return false
}

// contextKeepColumnsTables 是完整保留列的表数量上限。
const contextKeepColumnsTables = 8

// truncateMiddleRunes 按 rune 保留首尾，中间插入省略标记；不会切断 UTF-8。
func truncateMiddleRunes(s string, keep int) string {
	runes := []rune(s)
	if len(runes) <= keep || keep <= 0 {
		return s
	}
	head := keep * 2 / 3
	tail := keep - head
	return string(runes[:head]) + "\n…(" + itoa(len(runes)-keep) + " chars omitted)…\n" + string(runes[len(runes)-tail:])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// clipTailBytes 从尾部保留最多 maxBytes，并把起点对齐到 rune 边界。
func clipTailBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := len(s) - maxBytes
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}

// clipHeadBytes 从头部保留最多 maxBytes，并把终点对齐到 rune 边界。
func clipHeadBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// shrinkLargestMessage 是最后兜底：从最大的消息里按 rune 截掉一半。
func shrinkLargestMessage(msgs []Message) bool {
	idx, size := -1, 0
	for i, m := range msgs {
		if n := len(m.Content); n > size {
			idx, size = i, n
		}
	}
	if idx < 0 || size <= 64 {
		return false
	}
	runes := []rune(msgs[idx].Content)
	if len(runes) <= 64 {
		return false
	}
	msgs[idx].Content = truncateMiddleRunes(msgs[idx].Content, len(runes)/2)
	return true
}

// trimChatText 按优先级裁当前 SQL、选区、额外段；返回 false 表示没得裁。
func trimChatText(s *ContextSnapshot, extras *[]string, rep *PromptReport) bool {
	if len(s.ActiveSQL) > 64 {
		s.ActiveSQL = truncateMiddleRunes(s.ActiveSQL, len([]rune(s.ActiveSQL))/2)
		rep.TruncatedSQL = true
		return true
	}
	if len(s.Selection) > 64 {
		s.Selection = truncateMiddleRunes(s.Selection, len([]rune(s.Selection))/2)
		rep.TruncatedSelection = true
		return true
	}
	if s.Plan != nil && len(s.Plan.Text) > 128 {
		s.Plan.Text = truncateMiddleRunes(s.Plan.Text, len([]rune(s.Plan.Text))/2)
		rep.TruncatedPlan = true
		return true
	}
	for i, e := range *extras {
		if len(e) > 128 {
			(*extras)[i] = truncateMiddleRunes(e, len([]rune(e))/2)
			rep.TruncatedExtra = true
			return true
		}
	}
	return false
}

// finalizeReport 记录最终实际包含的规模。
func finalizeReport(s ContextSnapshot, rep *PromptReport, tokens int) PromptReport {
	rep.Tokens = tokens
	rep.Tables, rep.Columns, rep.Dialogue, rep.DDL = len(s.Tables), 0, len(s.Dialogue), 0
	for _, t := range s.Tables {
		rep.Columns += len(t.Columns)
		if t.DDL != "" {
			rep.DDL++
		}
	}
	if s.Result != nil {
		rep.ResultRows, rep.ResultColumns = s.Result.RowCount, len(s.Result.Columns)
	}
	rep.HasPlan = s.Plan != nil
	return *rep
}

// buildChatPrompt 是带预算的 Chat/Fix/Rewrite/Plan 公共组装器。
// extras 是追加到 user 消息末尾的受控段（失败 SQL、错误、计划等）。
func buildChatPrompt(snapshot ContextSnapshot, question string, extras []string, budget Budget) ([]Message, PromptReport) {
	rep := PromptReport{Limit: budget.Limit()}
	snapshot.Tables = RankTables(question, snapshot.Tables, snapshot.ActiveSQL, maxContextTables)
	render := func() []Message {
		msgs := chatMessages(snapshot, question)
		if len(extras) > 0 {
			msgs[1].Content += strings.Join(extras, "")
		}
		return msgs
	}
	msgs := render()
	tokens := tokenTotal(msgs)
	for tokens > rep.Limit {
		if trimSchemaStep(&snapshot, &rep) {
			msgs = render()
			tokens = tokenTotal(msgs)
			continue
		}
		if trimChatText(&snapshot, &extras, &rep) {
			msgs = render()
			tokens = tokenTotal(msgs)
			continue
		}
		if !shrinkLargestMessage(msgs) {
			break
		}
		rep.TruncatedText = true
		tokens = tokenTotal(msgs)
	}
	return msgs, finalizeReport(snapshot, &rep, tokens)
}

// buildCompletionPrompt 是内联补全的预算组装器：前文从尾部留、后文从头部留，
// 再按同样的表/列优先级裁剪。
func buildCompletionPrompt(snapshot ContextSnapshot, before, after string, budget Budget) ([]Message, PromptReport) {
	rep := PromptReport{Limit: budget.Limit()}
	const beforeLimit, afterLimit = 4000, 1200
	before = clipTailBytes(before, beforeLimit)
	after = clipHeadBytes(after, afterLimit)
	snapshot.Tables = RankTables(before, snapshot.Tables, snapshot.ActiveSQL, 12)
	render := func() []Message { return completionMessages(snapshot, before, after) }
	msgs := render()
	tokens := tokenTotal(msgs)
	for tokens > rep.Limit {
		if trimSchemaStep(&snapshot, &rep) {
			msgs = render()
			tokens = tokenTotal(msgs)
			continue
		}
		switch {
		case len(before) > 128:
			before = clipTailBytes(before, len(before)/2)
			rep.TruncatedText = true
		case len(after) > 128:
			after = clipHeadBytes(after, len(after)/2)
			rep.TruncatedText = true
		case !shrinkLargestMessage(msgs):
			return msgs, finalizeReport(snapshot, &rep, tokens)
		default:
			rep.TruncatedText = true
		}
		msgs = render()
		tokens = tokenTotal(msgs)
	}
	return msgs, finalizeReport(snapshot, &rep, tokens)
}
