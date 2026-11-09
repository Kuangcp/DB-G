package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"db-g/ai"
	"db-g/app"
)

// aiChatMessage 是 AI 面板里的一条消息。
type aiChatMessage struct {
	role string // "user" | "assistant"
	text string
	done bool
	err  string
}

// aiChatState 是右侧 AI 面板的瞬态状态。
type aiChatState struct {
	open       bool
	width      float32
	input      string
	messages   []aiChatMessage
	streaming  bool
	cancel     context.CancelFunc
	seq        uint64
	err        string
	lastCtx    string
	lastPrompt string
	ctxOpen    bool
	usage      ai.Usage
	started    time.Time
	md         markdown.Options
	scroll     ui.ScrollState
}

// aiProviderFor 返回当前配置对应的 Provider；测试可注入。
func (a *root) aiProviderFor() (ai.Provider, error) {
	if a.aiTestProvider != nil {
		return a.aiTestProvider, nil
	}
	base := strings.TrimSpace(a.aiPrefs.BaseURL)
	if base == "" {
		base = app.DefaultAIBaseURL
	}
	if strings.TrimSpace(a.aiPrefs.Model) == "" {
		return nil, errors.New("请先在设置 → AI 中填写模型名")
	}
	return ai.NewOpenAIProvider(ai.OpenAIConfig{
		BaseURL: base,
		APIKey:  a.aiPrefs.APIKey,
	}), nil
}

// aiSnapshot 组装一次请求的上下文；默认只带 schema 与当前 SQL。
func (a *root) aiSnapshot() ai.ContextSnapshot {
	var snap ai.ContextSnapshot
	connID := a.activeConnID()
	if a.store != nil && connID != "" {
		if p, ok := a.store.Get(connID); ok {
			snap.Connection = ai.ConnInfo{Kind: p.Kind, Name: p.Name, Database: p.Database}
		}
	}
	text := a.code.Text()
	start, _, hasSel := a.code.Selection()
	if st, ok := app.StatementAt(text, start); ok {
		snap.ActiveSQL = strings.TrimSpace(text[st.Start:st.End])
	}
	if hasSel {
		if sel, ok := a.code.SelectedText(); ok {
			snap.Selection = sel
		}
	}
	if a.catalog != nil {
		tables, views, ok := a.catalog.objects(connID)
		if ok {
			const maxTables = 200
			for _, group := range [][]string{tables, views} {
				for _, name := range group {
					if len(snap.Tables) >= maxTables {
						break
					}
					t := ai.TableContext{Name: name}
					if cols, ok := a.catalog.columns(connID, name); ok {
						for _, c := range cols {
							t.Columns = append(t.Columns, ai.ColumnContext{
								Name: c.Name, Type: c.Type,
								Nullable: c.Nullable, PrimaryKey: c.PrimaryKey,
							})
							if c.PrimaryKey {
								t.PrimaryKey = append(t.PrimaryKey, c.Name)
							}
						}
					}
					snap.Tables = append(snap.Tables, t)
				}
			}
		}
	}
	if a.aiPrefs.AllowHistory && a.history != nil && connID != "" {
		for _, h := range a.history.List(connID, 20) {
			snap.History = append(snap.History, ai.HistoryItem{
				SQL: h.SQL, OK: h.OK, Err: h.Err, Rows: h.Rows,
			})
		}
	}
	if a.aiPrefs.AllowErrorLogs && a.consoleErr != "" {
		snap.Errors = append(snap.Errors, ai.ErrorItem{SQL: snap.ActiveSQL, Err: a.consoleErr})
	}
	return snap
}

// sendAI 发送当前输入；服务端错误只写面板，不影响查询。
func (a *root) sendAI() {
	q := strings.TrimSpace(a.aiChat.input)
	if q == "" {
		return
	}
	snap := a.aiSnapshot()
	msgs := ai.BuildMessages(snap, q)
	tables, cols, rows := snap.Summary()
	a.aiChat.lastCtx = fmt.Sprintf("上下文：%d 张表 / %d 列 / %d 行样本", tables, cols, rows)
	if err := a.startAIRequest(q, msgs); err != nil {
		a.aiChat.err = err.Error()
		return
	}
	a.aiChat.input = ""
}

// startAIRequest 追加一轮消息并发起流式请求。
func (a *root) startAIRequest(display string, msgs []ai.Message) error {
	if a.aiChat.streaming {
		return errors.New("正在生成，请先停止")
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		return err
	}
	model := a.aiPrefs.Model
	if model == "" {
		model = "local"
	}
	if len(msgs) > 1 {
		a.aiChat.lastPrompt = msgs[0].Content + "\n\n---\n\n" + msgs[1].Content
	}
	a.aiChat.err = ""
	a.aiChat.usage = ai.Usage{}
	a.aiChat.messages = append(a.aiChat.messages,
		aiChatMessage{role: "user", text: display, done: true},
		aiChatMessage{role: "assistant"},
	)
	a.saveAIChat()
	a.aiChat.seq++
	seq := a.aiChat.seq
	ctx, cancel := context.WithCancel(context.Background())
	a.aiChat.cancel = cancel
	a.aiChat.streaming = true
	a.aiChat.started = time.Now()
	ch, err := provider.Stream(ctx, ai.Request{
		Model:       model,
		Messages:    msgs,
		Temperature: 0.2,
		MaxTokens:   1200,
	})
	if err != nil {
		a.aiChat.streaming = false
		a.aiChat.cancel = nil
		cancel()
		a.aiChat.err = err.Error()
		app.LogWarn("ai", "stream failed", "model", model, "err", err.Error())
		a.applyAIEvent(seq, ai.Event{Kind: ai.EventError, Err: err})
		return err
	}
	app.LogInfo("ai", "chat request", "model", model, "messages", len(msgs))
	a.consumeAI(seq, ch)
	a.aiChat.scroll.Y = math.MaxFloat32
	return nil
}

// consumeAI 消费事件流；真机把增量合并后回主线程，headless 同步应用。
func (a *root) consumeAI(seq uint64, ch <-chan ai.Event) {
	if a.win == nil {
		for ev := range ch {
			a.applyAIEvent(seq, ev)
		}
		return
	}
	win := a.win
	go func() {
		var buf strings.Builder
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		flush := func() {
			if buf.Len() == 0 {
				return
			}
			text := buf.String()
			buf.Reset()
			win.Update(func() { a.applyAIEvent(seq, ai.Event{Kind: ai.EventText, Text: text}) })
		}
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					flush()
					return
				}
				if ev.Kind == ai.EventText {
					buf.WriteString(ev.Text)
					continue
				}
				flush()
				event := ev
				win.Update(func() { a.applyAIEvent(seq, event) })
			case <-ticker.C:
				flush()
			}
		}
	}()
}

// applyAIEvent 应用一个流事件；旧请求直接忽略。
func (a *root) applyAIEvent(seq uint64, ev ai.Event) {
	if seq != a.aiChat.seq {
		return
	}
	n := len(a.aiChat.messages)
	if n == 0 || a.aiChat.messages[n-1].role != "assistant" {
		return
	}
	msg := &a.aiChat.messages[n-1]
	switch ev.Kind {
	case ai.EventText:
		msg.text += ev.Text
	case ai.EventUsage:
		if ev.Usage != nil {
			a.aiChat.usage = *ev.Usage
		}
	case ai.EventDone:
		msg.done = true
		a.aiChat.streaming = false
		a.aiChat.cancel = nil
		a.saveAIChat()
		app.LogInfo("ai", "chat done", "dur_ms", time.Since(a.aiChat.started).Milliseconds(),
			"tokens", a.aiChat.usage.TotalTokens)
	case ai.EventError:
		if ev.Err != nil {
			msg.err = ev.Err.Error()
			a.aiChat.err = ev.Err.Error()
			app.LogWarn("ai", "chat error", "err", ev.Err.Error())
		}
		msg.done = true
		a.aiChat.streaming = false
		a.aiChat.cancel = nil
		a.saveAIChat()
	}
}

// stopAI 取消当前请求并丢弃后续回包。
func (a *root) stopAI() {
	if !a.aiChat.streaming {
		return
	}
	a.aiChat.seq++
	if a.aiChat.cancel != nil {
		a.aiChat.cancel()
		a.aiChat.cancel = nil
	}
	a.aiChat.streaming = false
	if n := len(a.aiChat.messages); n > 0 {
		msg := &a.aiChat.messages[n-1]
		if msg.role == "assistant" && !msg.done {
			msg.done = true
			msg.err = "已停止"
		}
	}
	a.saveAIChat()
}

// resetAIChat 开始新会话。
func (a *root) resetAIChat() {
	a.stopAI()
	a.aiChat.messages = nil
	a.aiChat.err = ""
	a.aiChat.lastCtx = ""
	a.aiChat.usage = ai.Usage{}
	a.aiChat.scroll.Y = 0
	a.saveAIChat()
}

// insertAISQL 把消息里第一个 SQL 代码块插入编辑器。
func (a *root) insertAISQL(text string) bool {
	sql, ok := ai.ExtractSQL(text)
	if !ok {
		return false
	}
	a.code.Insert(sql)
	return true
}

// aiChatFile 返回当前工作区的 AI 会话文件；未配置或没有工作区时为空。
func (a *root) aiChatFile() string {
	if a.aiPrefsPath == "" {
		return ""
	}
	m := a.consoleMgr()
	if m == nil {
		return ""
	}
	ws, ok := m.ActiveWorkspace()
	if !ok {
		return ""
	}
	return app.AIConversationPath(filepath.Dir(a.aiPrefsPath), ws.ID)
}

// loadAIChat 读取当前工作区会话（打开面板时调用）。
func (a *root) loadAIChat() {
	conv, warn := app.LoadAIConversation(a.aiChatFile())
	if warn != "" {
		a.aiWarn = warn
	}
	a.aiChat.messages = a.aiChat.messages[:0]
	for _, m := range conv.Messages {
		a.aiChat.messages = append(a.aiChat.messages, aiChatMessage{
			role: m.Role, text: m.Text, done: m.Done, err: m.Err,
		})
	}
	a.aiChat.scroll.Y = math.MaxFloat32
}

// saveAIChat 保存当前工作区会话。
func (a *root) saveAIChat() {
	path := a.aiChatFile()
	if path == "" {
		return
	}
	conv := app.AIConversation{Version: 1}
	for _, m := range a.aiChat.messages {
		conv.Messages = append(conv.Messages, app.AIMessageRecord{
			Role: m.role, Text: m.text, Done: m.done, Err: m.err,
		})
	}
	if err := app.SaveAIConversation(path, conv); err != nil {
		a.aiWarn = "保存 AI 会话失败：" + err.Error()
	}
}

// saveAIPrefs 写盘 AI 配置；空路径（测试）不落盘。
func (a *root) saveAIPrefs() {
	if a.aiPrefsPath == "" {
		return
	}
	if err := app.SaveAIPrefs(a.aiPrefsPath, a.vault, a.aiPrefs); err != nil {
		a.aiWarn = "保存 AI 配置失败：" + err.Error()
		return
	}
	a.aiWarn = ""
}
