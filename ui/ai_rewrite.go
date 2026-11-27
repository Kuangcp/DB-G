package ui

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/ai"
	"db-g/app"
	"db-g/locales"
)

// aiRewriteState 是「AI 改写选中 SQL」弹窗的状态。
type aiRewriteState struct {
	open        bool
	loading     bool
	instruction string
	target      string // 打开时选中的原文
	start, end  int    // 原文在正文里的字节范围
	version     uint64 // 打开时的正文版本，应用前校验
	result      string
	err         string
	lint        aiSQLReport // 本地 SQL 自检
	seq         uint64
	cancel      context.CancelFunc
	diff        *myeditor.DiffState
}

// openAIRewrite 用当前选区（无选区时用光标所在语句）打开改写弹窗。
func (a *root) openAIRewrite() {
	text := a.code.Text()
	start, end, hasSel := a.code.Selection()
	if !hasSel || strings.TrimSpace(text[start:end]) == "" {
		st, ok := app.StatementAt(text, a.code.state.Cursor())
		if !ok || strings.TrimSpace(text[st.Start:st.End]) == "" {
			a.consoleErr = locales.AiRewriteSelectFirst()
			return
		}
		start, end = st.Start, st.End
	}
	a.ai.Rewrite = aiRewriteState{
		open: true, target: text[start:end],
		start: start, end: end, version: a.code.Version(),
	}
}

// startAIRewrite 发送一次改写请求；上下文与对话共用（同样的表结构、当前 SQL）。
func (a *root) startAIRewrite() {
	st := &a.ai.Rewrite
	instruction := strings.TrimSpace(st.instruction)
	if instruction == "" {
		st.err = locales.AiRewriteNeedInstruction()
		return
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		st.err = err.Error()
		return
	}
	snap := a.aiSnapshot()
	msgs, rep := ai.BuildRewrite(snap, st.target, instruction, a.aiBudget("rewrite", a.aiTaskMaxTokens("rewrite")))
	a.recordAIRequest(locales.AiKindRewrite(), a.aiTaskModel("fix"),
		aiContextNote(rep, locales.AiRewriteStats(len(st.target), len(instruction))), msgs)
	st.loading, st.err, st.result, st.lint, st.diff = true, "", "", aiSQLReport{}, nil
	ctx, cancel := context.WithCancel(context.Background())
	st.seq++
	seq := st.seq
	st.cancel = cancel
	req := ai.Request{
		Model:       a.aiTaskModel("fix"),
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   a.aiTaskMaxTokens("rewrite"),
	}
	read := func(ch <-chan ai.Event) (string, error) {
		var b strings.Builder
		var streamErr error
		for ev := range ch {
			switch ev.Kind {
			case ai.EventText:
				b.WriteString(ev.Text)
			case ai.EventError:
				streamErr = ev.Err
			}
		}
		return b.String(), streamErr
	}
	if a.win == nil {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			st.loading, st.cancel = false, nil
			cancel()
			st.err = err.Error()
			app.LogWarn("ai", "rewrite stream failed", "err", err.Error())
			return
		}
		text, err := read(ch)
		a.finishAIRewrite(seq, text, err)
		return
	}
	win := a.win
	go func() {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			if ctx.Err() == nil {
				app.LogWarn("ai", "rewrite stream failed", "err", err.Error())
				a.safeUpdate(win, func() { a.finishAIRewrite(seq, "", err) })
			}
			return
		}
		text, err := read(ch)
		a.safeUpdate(win, func() { a.finishAIRewrite(seq, text, err) })
	}()
}

// finishAIRewrite 在主线程落定模型结果并生成 diff。
func (a *root) finishAIRewrite(seq uint64, raw string, err error) {
	st := &a.ai.Rewrite
	if seq != st.seq {
		return
	}
	st.loading, st.cancel = false, nil
	if err != nil {
		st.err = err.Error()
		return
	}
	fixed, ok := ai.ExtractSQL(raw)
	if !ok || strings.TrimSpace(fixed) == "" {
		st.err = locales.AiNoSQLBlock()
		return
	}
	st.result = fixed
	st.lint = a.aiSQLCheck(fixed)
	lang := myeditor.LanguageFor("query.sql")
	old := myeditor.NewBuffer(st.target)
	old.SetLanguage(lang)
	next := myeditor.NewBuffer(fixed)
	next.SetLanguage(lang)
	st.diff = &myeditor.DiffState{
		Old: old, New: next,
		Layout:       myeditor.DiffUnified,
		Indicators:   myeditor.DiffClassic,
		ContextLines: 5,
	}
	a.recordAIRewriteConversation()
	app.LogInfo("ai", "rewrite ready", "in_len", len(st.target), "out_len", len(fixed))
}

// recordAIRewriteConversation 把这次 Ctrl+K 对话写进当前工作区的 AI 会话历史。
// 执行历史只记真实执行，改写结果不混进去。
func (a *root) recordAIRewriteConversation() {
	st := &a.ai.Rewrite
	instruction := strings.TrimSpace(st.instruction)
	if st.result == "" || instruction == "" {
		return
	}
	if !a.ensureAIChatStore() {
		return
	}
	now := time.Now().UnixMilli()
	title := locales.AiRewriteTitleFor(truncateRunes(firstLine(instruction), 28))
	conv := app.AIConversation{
		ID:          app.NewAIConversationID(),
		Title:       title,
		TitleLocked: true,
		CreatedAt:   now,
		UpdatedAt:   now,
		Messages: []app.AIMessageRecord{
			{Role: "user", Text: rewriteConversationUser(st.target, instruction), Done: true, Time: now},
			{Role: "assistant", Text: "```sql\n" + st.result + "\n```", Done: true, Time: now},
		},
	}
	a.ai.Chat.store.Conversations = append(a.ai.Chat.store.Conversations, conv)
	if a.ai.Chat.store.ActiveID == "" {
		a.ai.Chat.store.ActiveID = conv.ID
	}
	a.saveAIChat()
	app.LogInfo("ai", "rewrite conversation saved", "out_len", len(st.result))
}

// rewriteConversationUser 是写进 AI 历史里的用户消息；保留原文才看得出改了什么。
func rewriteConversationUser(target, instruction string) string {
	return locales.AiRewriteUserMessage(instruction, target)
}

// truncateRunes 截到 n 个字符并补省略号。
func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// stopAIRewrite 取消改写请求。
func (a *root) stopAIRewrite() {
	st := &a.ai.Rewrite
	if !st.loading {
		return
	}
	st.seq++
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.loading = false
}

// applyAIRewrite 把结果替换原文，或追加到原文之后；原文变了就拒绝。
func (a *root) applyAIRewrite(appendMode bool) {
	st := &a.ai.Rewrite
	if st.result == "" {
		return
	}
	text := a.code.Text()
	if a.code.Version() != st.version || st.start < 0 || st.end > len(text) ||
		st.start > st.end || text[st.start:st.end] != st.target {
		st.err = locales.AiRewriteChanged()
		return
	}
	if appendMode {
		insert := st.result
		if st.end > 0 && text[st.end-1] != '\n' {
			insert = "\n" + insert
		}
		a.code.Replace(st.end, st.end, insert)
	} else {
		a.code.Replace(st.start, st.end, st.result)
	}
	st.open = false
}

// aiRewriteModal 是 Ctrl+K 打开的改写浮窗。
func (a *root) aiRewriteModal(c *ui.Context) {
	st := &a.ai.Rewrite
	if !st.open {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &st.open, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.AiRewriteTitle()).Bold()
			ui.Spacer(c)
			ui.Text(c, locales.AiRewriteHint()).FontSize(11).TextColor(t.TextMuted)
		})
		ui.Text(c, locales.AiRewriteSendHint()).FontSize(11).TextColor(t.TextMuted)
		ui.TextArea(c, &st.instruction).
			Lines(2, 4).
			Placeholder(locales.AiRewritePlaceholder()).
			Label(locales.AiRewriteInstruction()).
			AutoFocus().
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == ui.Ctrl {
					a.startAIRewrite()
					return true
				}
				return false
			})
		ui.Text(c, locales.AiOriginal()).FontSize(11).TextColor(t.TextMuted)
		ui.TextArea(c, &st.target).ReadOnly(true).Lines(4, 8).FillWidth()
		switch {
		case st.loading:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.AiGenerating())
				ui.Spacer(c)
				if ui.Button(c, locales.AiStop()).Clicked() {
					a.stopAIRewrite()
				}
			})
		case st.err != "":
			ui.Text(c, st.err).TextColor(t.Danger).FontSize(12).SingleLine().Tooltip(st.err)
		}
		if !st.lint.Empty() {
			showAILintButton(a, c, st.lint)
		}
		if st.diff != nil {
			ui.Text(c, locales.AiDiff()).FontSize(11).TextColor(t.TextMuted)
			myeditor.DiffView(c, st.diff).Grow(1).FillWidth()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.stopAIRewrite()
				st.open = false
			}
			if st.result != "" && !st.loading {
				if ui.Button(c, locales.AiAppendAfter()).Clicked() {
					a.applyAIRewrite(true)
				}
				if ui.PrimaryButton(c, locales.AiReplaceSelection()).Clicked() {
					a.applyAIRewrite(false)
				}
			}
		})
	}).Width(920).Height(640)
	if m.Dismissed() {
		a.stopAIRewrite()
	}
}
