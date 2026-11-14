package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/ai"
	"db-g/app"
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
	seq         uint64
	cancel      context.CancelFunc
	diff        *myeditor.DiffState
	started     time.Time
	durMS       int64
}

// openAIRewrite 用当前选区（无选区时用光标所在语句）打开改写弹窗。
func (a *root) openAIRewrite() {
	text := a.code.Text()
	start, end, hasSel := a.code.Selection()
	if !hasSel || strings.TrimSpace(text[start:end]) == "" {
		st, ok := app.StatementAt(text, a.code.state.Cursor())
		if !ok || strings.TrimSpace(text[st.Start:st.End]) == "" {
			a.consoleErr = "先选中要改写的 SQL"
			return
		}
		start, end = st.Start, st.End
	}
	a.aiRewrite = aiRewriteState{
		open: true, target: text[start:end],
		start: start, end: end, version: a.code.Version(),
	}
}

// startAIRewrite 发送一次改写请求；上下文与对话共用（同样的表结构、当前 SQL）。
func (a *root) startAIRewrite() {
	st := &a.aiRewrite
	instruction := strings.TrimSpace(st.instruction)
	if instruction == "" {
		st.err = "先写一句改写要求"
		return
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		st.err = err.Error()
		return
	}
	snap := a.aiSnapshot()
	msgs := ai.BuildRewriteMessages(snap, st.target, instruction)
	a.recordAIRequest("改写", a.aiModelName(),
		aiContextNote(snap, fmt.Sprintf("待改写 %dB · 要求 %dB", len(st.target), len(instruction))), msgs)
	st.loading, st.err, st.result, st.diff = true, "", "", nil
	st.started = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	st.seq++
	seq := st.seq
	st.cancel = cancel
	req := ai.Request{
		Model:       a.aiPrefs.Model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   1600,
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
		ch, err := provider.Stream(ctx, req)
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
		ch, err := provider.Stream(ctx, req)
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
	st := &a.aiRewrite
	if seq != st.seq {
		return
	}
	st.loading, st.cancel = false, nil
	st.durMS = time.Since(st.started).Milliseconds()
	if err != nil {
		st.err = err.Error()
		return
	}
	fixed, ok := ai.ExtractSQL(raw)
	if !ok || strings.TrimSpace(fixed) == "" {
		st.err = "模型没有返回可用的 SQL 代码块"
		return
	}
	st.result = fixed
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
	app.LogInfo("ai", "rewrite ready", "in_len", len(st.target), "out_len", len(fixed))
}

// stopAIRewrite 取消改写请求。
func (a *root) stopAIRewrite() {
	st := &a.aiRewrite
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
	st := &a.aiRewrite
	if st.result == "" {
		return
	}
	text := a.code.Text()
	if a.code.Version() != st.version || st.start < 0 || st.end > len(text) ||
		st.start > st.end || text[st.start:st.end] != st.target {
		st.err = "原文已变化，请重新选择后再试"
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
	a.addAIRewriteHistory(appendMode)
	st.open = false
}

// addAIRewriteHistory 把应用后的 SQL 记入当前数据源的执行历史，用 Kind 区分。
func (a *root) addAIRewriteHistory(appended bool) {
	st := &a.aiRewrite
	if a.history == nil || strings.TrimSpace(st.result) == "" {
		return
	}
	mode := "替换选区"
	if appended {
		mode = "追加到选区后"
	}
	note := strings.TrimSpace(st.instruction)
	if note == "" {
		note = mode
	} else {
		note += "（" + mode + "）"
	}
	e := app.HistoryEntry{
		ConnID: a.activeConnID(), ConnName: a.activeConnName(),
		SQL: st.result, DurMS: st.durMS, OK: true,
		Kind: app.HistoryKindAIRewrite, Note: note,
	}
	if err := a.history.Add(e); err != nil {
		app.LogWarn("ai", "rewrite history failed", "err", err.Error())
		return
	}
	app.LogInfo("ai", "rewrite applied", "mode", mode, "out_len", len(st.result))
}

// aiRewriteModal 是 Ctrl+K 打开的改写浮窗。
func (a *root) aiRewriteModal(c *ui.Context) {
	st := &a.aiRewrite
	if !st.open {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &st.open, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "AI 改写 SQL").Bold()
			ui.Spacer(c)
			ui.Text(c, "生成结果不会自动执行；应用前可看 diff").FontSize(11).TextColor(t.TextMuted)
		})
		ui.Text(c, "Ctrl+Enter 发送指令；应用前不会改动正文").FontSize(11).TextColor(t.TextMuted)
		ui.TextArea(c, &st.instruction).
			Lines(2, 4).
			Placeholder("例如：改成参数化查询 / 只保留需要的列 / 加上时间范围过滤").
			Label("改写要求").
			AutoFocus().
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == ui.Ctrl {
					a.startAIRewrite()
					return true
				}
				return false
			})
		ui.Text(c, "原文").FontSize(11).TextColor(t.TextMuted)
		ui.TextArea(c, &st.target).ReadOnly(true).Lines(4, 8).FillWidth()
		switch {
		case st.loading:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "生成中…")
				ui.Spacer(c)
				if ui.Button(c, "停止").Clicked() {
					a.stopAIRewrite()
				}
			})
		case st.err != "":
			ui.Text(c, st.err).TextColor(t.Danger).FontSize(12).SingleLine().Tooltip(st.err)
		}
		if st.diff != nil {
			ui.Text(c, "替换后的 diff").FontSize(11).TextColor(t.TextMuted)
			myeditor.DiffView(c, st.diff).Grow(1).FillWidth()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.stopAIRewrite()
				st.open = false
			}
			if st.result != "" && !st.loading {
				if ui.Button(c, "追加到选区后").Clicked() {
					a.applyAIRewrite(true)
				}
				if ui.PrimaryButton(c, "替换选区").Clicked() {
					a.applyAIRewrite(false)
				}
			}
		})
	}).Width(920).Height(640)
	if m.Dismissed() {
		a.stopAIRewrite()
	}
}
