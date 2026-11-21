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
	open           bool
	sel            int // -1 = 发送前预览，>=0 = 最近请求
	entries        []aiDebugEntry
	draft          *aiDebugEntry // 发送前预览；发送后清空
	previewOpts    aiEgressPolicy
	previewAllowed aiEgressPolicy
	body           string
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
	st.draft = nil
	st.entries = append([]aiDebugEntry{{
		Kind: kind, Model: model, Note: note, At: time.Now(),
		Messages: append([]ai.Message(nil), msgs...), Tokens: total,
	}}, st.entries...)
	if len(st.entries) > maxAIDebugEntries {
		st.entries = st.entries[:maxAIDebugEntries]
	}
	if st.open {
		if st.sel < 0 {
			st.sel = 0
		} else {
			st.sel++
		}
	}
	if st.sel >= len(st.entries) {
		st.sel = len(st.entries) - 1
	}
	if st.sel < 0 {
		st.sel = 0
	}
	a.selectAIDebug(st.sel)
}

// aiBudget 把设置里的上下文窗口和本次输出上限合成一次请求预算。
func (a *root) aiBudget(task string, maxOutput int) ai.Budget {
	scale := 1.0
	if v := a.aiTokenScale[a.aiTaskModel(task)]; v > 1 {
		scale = v
	}
	return ai.Budget{ContextWindow: a.aiPrefs.ContextWindow, MaxOutput: maxOutput, EstimateScale: scale}
}

// aiModelName 返回请求里实际写的模型名；未配置时与请求一致记为 local。
func (a *root) aiModelName() string {
	if m := strings.TrimSpace(a.aiPrefs.Model); m != "" {
		return m
	}
	return "local"
}

// aiContextNote 概括一次请求实际发送的上下文规模与裁剪情况。
func aiContextNote(rep ai.PromptReport, extra string) string {
	parts := []string{
		locales.AiDebugTablesCols(rep.Tables, rep.Columns),
		locales.AiContextTokens(rep.Tokens, rep.Limit),
	}
	if rep.Dialogue > 0 {
		parts = append(parts, locales.AiDebugDialogue(rep.Dialogue))
	}
	if rep.ResultRows > 0 || rep.ResultColumns > 0 {
		parts = append(parts, locales.AiDebugResult(rep.ResultRows, rep.ResultColumns))
	}
	if rep.HasPlan {
		parts = append(parts, locales.AiDebugPlan())
	}
	if rep.DDL > 0 {
		parts = append(parts, locales.AiDebugDDL(rep.DDL))
	}
	if rep.Trimmed() {
		parts = append(parts, locales.AiContextDropped(
			rep.DroppedTables, rep.DroppedColumns, rep.DroppedDialogue, rep.DroppedDDL,
			rep.DroppedHistory, rep.DroppedErrors, rep.DroppedSamples))
		if rep.TruncatedSQL || rep.TruncatedSelection || rep.TruncatedExtra || rep.TruncatedPlan || rep.TruncatedText {
			parts = append(parts, locales.AiContextTextTrimmed())
		}
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

// openAIPreview 组装“现在发送会发什么”的草稿，不改编辑器、不发请求。
func (a *root) openAIPreview() {
	q := strings.TrimSpace(a.aiChat.input)
	allowed := a.aiEgressPolicy()
	a.aiDebug.previewAllowed = allowed
	a.aiDebug.previewOpts = allowed
	a.startAIPreview(q)
}

// refreshAIPreview 在预览开关变化后重算草稿；旧的后台加载作废。
func (a *root) refreshAIPreview() {
	a.cancelAIPrepare()
	a.startAIPreview(strings.TrimSpace(a.aiChat.input))
}

func (a *root) startAIPreview(q string) {
	opts := a.aiDebug.previewOpts
	snap := a.chatSnapshotWith(opts)
	a.prepareAIContext(q, snap, opts.ddl, func(snap ai.ContextSnapshot) { a.showAIPreview(q, snap) })
}

// showAIPreview 用准备好的快照打开预览；DDL 加载期间面板会显示准备状态。
func (a *root) showAIPreview(q string, snap ai.ContextSnapshot) {
	msgs, rep := ai.BuildChat(snap, q, a.aiBudget("chat", 1200))
	note := aiContextNote(rep, "")
	if q == "" {
		note += " · " + locales.AiPreviewNoQuestion()
	}
	a.aiDebug.draft = &aiDebugEntry{
		Kind: locales.AiPreviewDraft(), Model: a.aiModelName(), Note: note,
		At: time.Now(), Messages: msgs, Tokens: rep.Tokens,
	}
	a.aiDebug.open = true
	a.selectAIDebug(-1)
}

// selectAIDebug 选中一条请求并把消息渲染成可复制的纯文本。
func (a *root) selectAIDebug(idx int) {
	st := &a.aiDebug
	if idx < -1 || idx >= len(st.entries) {
		return
	}
	var e aiDebugEntry
	if idx == -1 {
		if st.draft == nil {
			return
		}
		e = *st.draft
	} else {
		e = st.entries[idx]
	}
	st.sel = idx
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
	title := locales.AiDebugTitle()
	if st.sel == -1 {
		title = locales.AiPreviewDraft()
	}
	ui.Modal(c, &st.open, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).Bold()
			ui.Text(c, locales.AiDebugHint()).FontSize(11).TextColor(t.TextMuted)
		})
		if len(st.entries) == 0 && st.draft == nil {
			ui.Spacer(c)
			return
		}
		if st.sel == -1 {
			ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
				check := func(label, tip string, value *bool, allowed bool) {
					if ui.Checkbox(c, value, label).FontSize(11).Disabled(!allowed).Tooltip(tip).Changed() {
						a.refreshAIPreview()
					}
				}
				check(locales.AiCtxDDL(), locales.AiAllowDDL(), &st.previewOpts.ddl, st.previewAllowed.ddl)
				check(locales.AiCtxSamples(), locales.AiAllowSamples(), &st.previewOpts.samples, st.previewAllowed.samples)
				check(locales.AiCtxHistory(), locales.AiAllowHistory(), &st.previewOpts.history, st.previewAllowed.history)
				check(locales.AiCtxErrors(), locales.AiAllowErrors(), &st.previewOpts.errors, st.previewAllowed.errors)
				ui.Spacer(c)
				if a.aiChat.preparing {
					ui.Spinner(c)
					ui.Text(c, locales.AiPreparingContext()).FontSize(10.5).TextColor(t.TextMuted)
				}
			})
		}
		ui.Row(c).Grow(1).FillWidth().Gap(10).Children(func() {
			ui.Column(c).Width(220).FillHeight().Children(func() {
				if st.draft != nil {
					label := fmt.Sprintf("%s · ~%d tokens", locales.AiPreviewDraft(), st.draft.Tokens)
					if row := sideNavRow(c, label, nil, ui.Transparent, st.sel == -1); row.Clicked() {
						a.selectAIDebug(-1)
					}
				}
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
