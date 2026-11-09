package ui

import (
	"context"
	"strings"

	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/ai"
	"db-g/app"
)

// aiCompletionState 是一次内联补全请求的状态。
type aiCompletionState struct {
	seq        uint64
	loading    bool
	offset     int
	version    uint64
	prefix     string
	suggestion string
	cancel     context.CancelFunc
}

// requestAICompletion 以当前光标为位置发起一次 AI 补全。
func (a *root) requestAICompletion() {
	provider, err := a.aiProviderFor()
	if err != nil {
		a.consoleErr = err.Error()
		return
	}
	text := a.code.Text()
	offset := a.code.state.Cursor()
	if offset < 0 || offset > len(text) {
		return
	}
	start := offset
	for start > 0 && isIdentPart(text[start-1]) {
		start--
	}
	st := &a.aiCompletion
	if st.cancel != nil {
		st.cancel()
	}
	st.seq++
	seq := st.seq
	st.offset, st.version = offset, a.code.Version()
	st.prefix = text[start:offset]
	st.suggestion = ""
	st.loading = true
	msgs := ai.BuildCompletionMessages(a.aiSnapshot(), text[:offset], text[offset:])
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel = cancel
	req := ai.Request{
		Model:       a.aiPrefs.Model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   160,
	}
	consume := func(text string, err error) {
		if seq != a.aiCompletion.seq {
			return
		}
		a.aiCompletion.loading = false
		a.aiCompletion.cancel = nil
		if err != nil {
			a.consoleErr = err.Error()
			return
		}
		got := ai.ExtractCompletion(text)
		if got == "" {
			return
		}
		a.aiCompletion.suggestion = got
		a.showAICompletion(seq)
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
			st.loading = false
			st.cancel = nil
			cancel()
			a.consoleErr = err.Error()
			app.LogWarn("ai", "completion failed", "err", err.Error())
			return
		}
		text, err := read(ch)
		consume(text, err)
		return
	}
	win := a.win
	go func() {
		ch, err := provider.Stream(ctx, req)
		if err != nil {
			if ctx.Err() == nil {
				app.LogWarn("ai", "completion failed", "err", err.Error())
				a.safeUpdate(win, func() { consume("", err) })
			}
			return
		}
		text, err := read(ch)
		a.safeUpdate(win, func() { consume(text, err) })
	}()

}

// showAICompletion 在编辑器里弹出 AI 候选；光标/版本变了就丢弃。
func (a *root) showAICompletion(seq uint64) {
	st := &a.aiCompletion
	if seq != st.seq || st.suggestion == "" {
		return
	}
	if a.code.Version() != st.version || a.code.state.Cursor() != st.offset {
		return
	}
	label := st.suggestion
	if i := strings.IndexByte(label, '\n'); i >= 0 {
		label = label[:i] + " …"
	}
	if len([]rune(label)) > 60 {
		label = string([]rune(label)[:60]) + "…"
	}
	if strings.TrimSpace(label) == "" {
		label = "AI"
	}
	items := a.completionItemsAt(st.offset)
	item := myeditor.Completion{
		Label:         label,
		Insert:        st.suggestion,
		Detail:        "AI",
		Kind:          myeditor.CompletionSnippet,
		FilterText:    st.prefix,
		Documentation: "AI 补全（Alt+\\）",
	}
	start := st.offset - len(st.prefix)
	a.code.state.ShowCompletions(start, append([]myeditor.Completion{item}, items...))
	app.LogInfo("ai", "completion ready", "chars", len(st.suggestion))
}
