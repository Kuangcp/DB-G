package ui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/ai"
	"db-g/app"
	"db-g/locales"
)

// aiFixState 是 SQL 错误修复弹窗的状态。
type aiFixState struct {
	open     bool
	loading  bool
	original string
	fixed    string
	err      string
	lint     aiSQLReport // 本地 SQL 自检
	seq      uint64
	cancel   context.CancelFunc
	diff     *myeditor.DiffState
}

// currentRunSQL 返回当前结果页对应的 SQL。
func (a *root) currentRunSQL() string {
	if len(a.tabs.StmtRuns) > 0 && a.tabs.ActiveStmt >= 0 && a.tabs.ActiveStmt < len(a.tabs.StmtRuns) {
		if sql := strings.TrimSpace(a.tabs.StmtRuns[a.tabs.ActiveStmt].sql); sql != "" {
			return sql
		}
	}
	return strings.TrimSpace(a.tabs.LastExecSQL)
}

// fixErrorSQL 用 AI 修复当前失败语句。
func (a *root) fixErrorSQL() {
	sql := a.currentRunSQL()
	errText := a.currentRuntime().Err()
	if sql == "" || errText == "" {
		return
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		a.ai.Fix = aiFixState{open: true, err: err.Error(), original: sql}
		return
	}
	snap := a.aiSnapshot()
	msgs, rep := ai.BuildFix(snap, sql, errText, a.aiBudget("fix", a.aiTaskMaxTokens("fix")))
	a.recordAIRequest(locales.AiKindFix(), a.aiTaskModel("fix"),
		aiContextNote(rep, locales.AiFailureStats(len(sql), len(errText))), msgs)
	a.ai.Fix = aiFixState{open: true, loading: true, original: sql}
	ctx, cancel := context.WithCancel(context.Background())
	a.ai.Fix.seq++
	seq := a.ai.Fix.seq
	a.ai.Fix.cancel = cancel
	req := ai.Request{
		Model:       a.aiTaskModel("fix"),
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   a.aiTaskMaxTokens("fix"),
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
			a.ai.Fix.loading = false
			a.ai.Fix.err = err.Error()
			a.ai.Fix.cancel = nil
			cancel()
			app.LogWarn("ai", "fix stream failed", "err", err.Error())
			return
		}
		text, err := read(ch)
		a.finishAIFix(seq, text, err)
		return
	}
	win := a.win
	go func() {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			if ctx.Err() == nil {
				app.LogWarn("ai", "fix stream failed", "err", err.Error())
				a.safeUpdate(win, func() { a.finishAIFix(seq, "", err) })
			}
			return
		}
		app.LogInfo("ai", "fix request", "model", a.aiTaskModel("fix"), "sql_len", len(sql))
		text, err := read(ch)
		a.safeUpdate(win, func() { a.finishAIFix(seq, text, err) })
	}()

}

// finishAIFix 在主线程落定模型结果。
func (a *root) finishAIFix(seq uint64, raw string, err error) {
	if seq != a.ai.Fix.seq {
		return
	}
	a.ai.Fix.loading = false
	a.ai.Fix.cancel = nil
	if err != nil {
		a.ai.Fix.err = err.Error()
		return
	}
	fixed, ok := ai.ExtractSQL(raw)
	if !ok || strings.TrimSpace(fixed) == "" {
		a.ai.Fix.err = locales.AiNoSQLBlock()
		return
	}
	a.ai.Fix.fixed = fixed
	a.ai.Fix.lint = a.aiSQLCheck(fixed)
	lang := myeditor.LanguageFor("query.sql")
	old := myeditor.NewBuffer(a.ai.Fix.original)
	old.SetLanguage(lang)
	next := myeditor.NewBuffer(fixed)
	next.SetLanguage(lang)
	a.ai.Fix.diff = &myeditor.DiffState{
		Old: old, New: next,
		Layout:       myeditor.DiffUnified,
		Indicators:   myeditor.DiffClassic,
		ContextLines: 5,
	}
	app.LogInfo("ai", "fix ready", "sql_len", len(fixed))
}

// stopAIFix 取消修复请求。
func (a *root) stopAIFix() {
	if !a.ai.Fix.loading {
		return
	}
	a.ai.Fix.seq++
	if a.ai.Fix.cancel != nil {
		a.ai.Fix.cancel()
		a.ai.Fix.cancel = nil
	}
	a.ai.Fix.loading = false
}

// applyAIFix 把修复结果替换回原语句；run 为 true 时随后执行。
func (a *root) applyAIFix(run bool) {
	if strings.TrimSpace(a.ai.Fix.fixed) == "" {
		return
	}
	if !a.replaceStatement(a.ai.Fix.original, a.ai.Fix.fixed) {
		a.ai.Fix.err = locales.AiSourceChanged()
		return
	}
	a.ai.Fix.open = false
	if run {
		a.deferUI(a.runSQL)
	}
}

// replaceStatement 用 fixed 替换编辑器里的 original 语句。
func (a *root) replaceStatement(original, fixed string) bool {
	text := a.code.Text()
	original = strings.TrimSpace(original)
	if original == "" {
		return false
	}
	if st, ok := app.StatementAt(text, a.code.state.Cursor()); ok {
		if strings.TrimSpace(text[st.Start:st.End]) == original {
			a.code.Replace(st.Start, st.End, fixed)
			return true
		}
	}
	var match *app.SQLStatement
	count := 0
	for _, st := range app.SplitStatements(text) {
		if strings.TrimSpace(text[st.Start:st.End]) == original {
			s := st
			match = &s
			count++
		}
	}
	if count == 1 {
		a.code.Replace(match.Start, match.End, fixed)
		return true
	}
	return false
}

// aiFixModal 展示修复 diff 并确认应用。
func (a *root) aiFixModal(c *ui.Context) {
	if !a.ai.Fix.open {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &a.ai.Fix.open, func() {
		ui.Text(c, locales.AiFixTitle()).Bold()
		switch {
		case a.ai.Fix.loading:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.AiFixGenerating())
				ui.Spacer(c)
				if ui.Button(c, locales.AiStop()).Clicked() {
					a.stopAIFix()
				}
			})
		case a.ai.Fix.err != "":
			ui.Text(c, a.ai.Fix.err).TextColor(t.Danger).FontSize(12)
		}
		if !a.ai.Fix.lint.Empty() {
			showAILintButton(a, c, a.ai.Fix.lint)
		}
		if a.ai.Fix.diff != nil {
			myeditor.DiffView(c, a.ai.Fix.diff).Grow(1).FillWidth()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.CommonCancel()).Clicked() {
				a.stopAIFix()
				a.ai.Fix.open = false
			}
			if a.ai.Fix.fixed != "" {
				if ui.Button(c, locales.AiApplyFix()).Clicked() {
					a.applyAIFix(false)
				}
				if ui.PrimaryButton(c, locales.AiApplyRun()).Clicked() {
					a.applyAIFix(true)
				}
			}
		})
	}).Width(920).Height(620)
	if m.Dismissed() {
		a.stopAIFix()
	}
}
