package ui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"
	myeditor "github.com/egoist/mygo/ui/editor"

	"db-g/ai"
	"db-g/app"
)

// aiFixState 是 SQL 错误修复弹窗的状态。
type aiFixState struct {
	open     bool
	loading  bool
	original string
	fixed    string
	err      string
	seq      uint64
	cancel   context.CancelFunc
	diff     *myeditor.DiffState
}

// currentRunSQL 返回当前结果页对应的 SQL。
func (a *root) currentRunSQL() string {
	if len(a.stmtRuns) > 0 && a.activeStmt >= 0 && a.activeStmt < len(a.stmtRuns) {
		if sql := strings.TrimSpace(a.stmtRuns[a.activeStmt].sql); sql != "" {
			return sql
		}
	}
	return strings.TrimSpace(a.lastExecSQL)
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
		a.aiFix = aiFixState{open: true, err: err.Error(), original: sql}
		return
	}
	msgs := ai.BuildFixMessages(a.aiSnapshot(), sql, errText)
	a.aiFix = aiFixState{open: true, loading: true, original: sql}
	ctx, cancel := context.WithCancel(context.Background())
	a.aiFix.seq++
	seq := a.aiFix.seq
	a.aiFix.cancel = cancel
	ch, err := provider.Stream(ctx, ai.Request{
		Model:       a.aiPrefs.Model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   1200,
	})
	if err != nil {
		a.aiFix.loading = false
		a.aiFix.err = err.Error()
		a.aiFix.cancel = nil
		cancel()
		app.LogWarn("ai", "fix stream failed", "err", err.Error())
		return
	}
	app.LogInfo("ai", "fix request", "model", a.aiPrefs.Model, "sql_len", len(sql))
	consume := func() {
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
		a.finishAIFix(seq, b.String(), streamErr)
	}
	if a.win == nil {
		consume()
		return
	}
	win := a.win
	go func() {
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
		text := b.String()
		win.Update(func() { a.finishAIFix(seq, text, streamErr) })
	}()
}

// finishAIFix 在主线程落定模型结果。
func (a *root) finishAIFix(seq uint64, raw string, err error) {
	if seq != a.aiFix.seq {
		return
	}
	a.aiFix.loading = false
	a.aiFix.cancel = nil
	if err != nil {
		a.aiFix.err = err.Error()
		return
	}
	fixed, ok := ai.ExtractSQL(raw)
	if !ok || strings.TrimSpace(fixed) == "" {
		a.aiFix.err = "模型没有返回可用的 SQL 代码块"
		return
	}
	a.aiFix.fixed = fixed
	lang := myeditor.LanguageFor("query.sql")
	old := myeditor.NewBuffer(a.aiFix.original)
	old.SetLanguage(lang)
	next := myeditor.NewBuffer(fixed)
	next.SetLanguage(lang)
	a.aiFix.diff = &myeditor.DiffState{
		Old: old, New: next,
		Layout:       myeditor.DiffUnified,
		Indicators:   myeditor.DiffClassic,
		ContextLines: 5,
	}
	app.LogInfo("ai", "fix ready", "sql_len", len(fixed))
}

// stopAIFix 取消修复请求。
func (a *root) stopAIFix() {
	if !a.aiFix.loading {
		return
	}
	a.aiFix.seq++
	if a.aiFix.cancel != nil {
		a.aiFix.cancel()
		a.aiFix.cancel = nil
	}
	a.aiFix.loading = false
}

// applyAIFix 把修复结果替换回原语句；run 为 true 时随后执行。
func (a *root) applyAIFix(run bool) {
	if strings.TrimSpace(a.aiFix.fixed) == "" {
		return
	}
	if !a.replaceStatement(a.aiFix.original, a.aiFix.fixed) {
		a.aiFix.err = "原语句已被修改，未应用修复"
		return
	}
	a.aiFix.open = false
	if run {
		a.execPending = true
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
	if !a.aiFix.open {
		return
	}
	t := c.Theme()
	m := ui.Modal(c, &a.aiFix.open, func() {
		ui.Text(c, "AI 修复 SQL").Bold()
		switch {
		case a.aiFix.loading:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "生成修复建议…")
				ui.Spacer(c)
				if ui.Button(c, "停止").Clicked() {
					a.stopAIFix()
				}
			})
		case a.aiFix.err != "":
			ui.Text(c, a.aiFix.err).TextColor(t.Danger).FontSize(12)
		}
		if a.aiFix.diff != nil {
			myeditor.DiffView(c, a.aiFix.diff).Grow(1).FillWidth()
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.stopAIFix()
				a.aiFix.open = false
			}
			if a.aiFix.fixed != "" {
				if ui.Button(c, "应用修复").Clicked() {
					a.applyAIFix(false)
				}
				if ui.PrimaryButton(c, "应用并执行").Clicked() {
					a.applyAIFix(true)
				}
			}
		})
	}).Width(920).Height(620)
	if m.Dismissed() {
		a.stopAIFix()
	}
}
