package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"db-g/ai"
	"db-g/locales"
)

// maxAIDebugEntries 是内存里保留的最近 AI 请求数；只用于排查上下文，不落盘。
const maxAIDebugEntries = 12

// aiDebugEntry 是一次真正发往模型的请求快照。
type aiDebugEntry struct {
	Kind     string
	Model    string
	Note     string
	At       time.Time
	Messages []ai.Message
	Tokens   int // 所有消息的粗略 token 估算
}

// aiDebugState 是「最近 AI 请求」查看器的瞬态状态。
type aiDebugState struct {
	open    bool
	sel     int
	entries []aiDebugEntry
	body    string
}

// recordAIRequest 记录一次请求，供「查看上下文」剖析实际发送的内容。
// 只看内存，不写日志也不落盘：提示词里可能含用户字面量与表结构。
func (a *root) recordAIRequest(kind, model, note string, msgs []ai.Message) {
	if len(msgs) == 0 {
		return
	}
	total := 0
	for _, m := range msgs {
		total += ai.EstimateTokens(m.Content)
	}
	st := &a.aiDebug
	st.entries = append([]aiDebugEntry{{
		Kind: kind, Model: model, Note: note, At: time.Now(),
		Messages: append([]ai.Message(nil), msgs...), Tokens: total,
	}}, st.entries...)
	if len(st.entries) > maxAIDebugEntries {
		st.entries = st.entries[:maxAIDebugEntries]
	}
	if st.open && st.sel > 0 {
		st.sel++
	}
	if st.sel >= len(st.entries) {
		st.sel = len(st.entries) - 1
	}
	if st.sel < 0 {
		st.sel = 0
	}
	a.selectAIDebug(st.sel)
}

// aiModelName 返回请求里实际写的模型名；未配置时与请求一致记为 local。
func (a *root) aiModelName() string {
	if m := strings.TrimSpace(a.aiPrefs.Model); m != "" {
		return m
	}
	return "local"
}

// aiContextNote 概括一次请求的上下文规模，方便对照模型实际收到的提示词。
func aiContextNote(snap ai.ContextSnapshot, extra string) string {
	tables, cols, _ := snap.Summary()
	empty := 0
	for _, t := range snap.Tables {
		if len(t.Columns) == 0 {
			empty++
		}
	}
	var parts []string
	parts = append(parts, locales.AiDebugTablesCols(tables, cols))
	if empty > 0 {
		parts = append(parts, locales.AiDebugEmptyColumns(empty))
	}
	if len(snap.History) > 0 {
		parts = append(parts, locales.AiDebugHistory(len(snap.History)))
	}
	if len(snap.Errors) > 0 {
		parts = append(parts, locales.AiDebugErrors(len(snap.Errors)))
	}
	if s := strings.TrimSpace(snap.ActiveSQL); s != "" {
		parts = append(parts, locales.AiDebugSQLBytes(len(s)))
	}
	if s := strings.TrimSpace(snap.Selection); s != "" {
		parts = append(parts, locales.AiDebugSelectionBytes(len(s)))
	}
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " · ")
}

// openAIDebug 打开最近请求查看器；有请求时默认看最新一条。
func (a *root) openAIDebug() {
	a.aiDebug.open = true
	if len(a.aiDebug.entries) == 0 {
		a.aiDebug.body = locales.AiDebugEmpty()
		return
	}
	a.aiDebug.sel = 0
	a.selectAIDebug(0)
}

// selectAIDebug 选中一条请求并把消息渲染成可复制的纯文本。
func (a *root) selectAIDebug(idx int) {
	st := &a.aiDebug
	if idx < 0 || idx >= len(st.entries) {
		return
	}
	st.sel = idx
	e := st.entries[idx]
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · ~%d tokens\n", e.Kind, e.At.Format("2006-01-02 15:04:05"), e.Tokens)
	if e.Model != "" {
		fmt.Fprintf(&b, "%s\n", locales.AiDebugModel(e.Model))
	}
	if e.Note != "" {
		fmt.Fprintf(&b, "%s\n", locales.AiDebugContext(e.Note))
	}
	for i, m := range e.Messages {
		fmt.Fprintf(&b, "\n===== [%d] %s · ~%d tokens =====\n%s\n", i+1, m.Role, ai.EstimateTokens(m.Content), m.Content)
	}
	st.body = b.String()
}

// aiDebugModal 展示最近请求的完整消息，用来核对上下文是否冗余或缺失。
func (a *root) aiDebugModal(c *ui.Context) {
	st := &a.aiDebug
	if !st.open {
		return
	}
	t := c.Theme()
	ui.Modal(c, &st.open, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.AiDebugTitle()).Bold()
			ui.Text(c, locales.AiDebugHint()).FontSize(11).TextColor(t.TextMuted)
		})
		if len(st.entries) == 0 {
			ui.Spacer(c)
			return
		}
		ui.Row(c).Grow(1).FillWidth().Gap(10).Children(func() {
			ui.Column(c).Width(220).FillHeight().Children(func() {
				for i, e := range st.entries {
					label := fmt.Sprintf("%s · %s", e.Kind, e.At.Format("15:04:05"))
					if row := sideNavRow(c, label, nil, ui.Transparent, i == st.sel); row.Clicked() {
						a.selectAIDebug(i)
					}
				}
				ui.Spacer(c)
				ui.Text(c, locales.AiDebugTokenHint()).FontSize(10.5).TextColor(t.TextMuted)
			})
			ui.Box(c).Width(1).FillHeight().Background(t.Border)
			ui.Column(c).Grow(1).FillHeight().Gap(6).Children(func() {
				ui.TextArea(c, &st.body).ReadOnly(true).Lines(20, 30).Grow(1).FillWidth()
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, locales.AiDebugCopyAll()).Clicked() {
						c.WriteClipboard(st.body)
					}
					if ui.Button(c, locales.CommonClose()).Clicked() {
						st.open = false
					}
				})
			})
		})
	}).Width(900).Height(640)
}
