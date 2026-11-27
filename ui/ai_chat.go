package ui

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"db-g/ai"
	"db-g/app"
	"db-g/engine"
	"db-g/locales"
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
	split      float32 // 主区域占左右分屏的百分比；剩余给 AI 面板
	input      string
	messages   []aiChatMessage
	streaming  bool
	cancel     context.CancelFunc
	seq        uint64
	err        string
	lastCtx    string
	lint       aiSQLReport // 最近一次插入 SQL 的本地自检结果
	preparing  bool        // 正在补齐 DDL 等上下文
	prepareSeq uint64      // 上下文准备轮次；旧结果作废
	usage      ai.Usage
	totalUsage ai.Usage
	estimate   int // 本次请求的估算输入 token
	reqModel   string

	usageCounted  bool
	started       time.Time
	md            markdown.Options
	scroll        ui.ScrollState
	store         app.AIConversationStore
	storeLoaded   bool
	storePath     string
	convID        string
	showHistory   bool
	historyQuery  string
	historyLimit  int
	historyScroll ui.ScrollState
	inputRef      ui.Handle
}

// aiProviderFor 返回当前配置对应的 Provider；测试可注入。
func (a *root) aiProviderFor() (ai.Provider, error) {
	if a.ai.TestProvider != nil {
		return a.ai.TestProvider, nil
	}
	base := strings.TrimSpace(a.ai.Prefs.BaseURL)
	if base == "" {
		base = app.DefaultAIBaseURL
	}
	if strings.TrimSpace(a.ai.Prefs.Model) == "" {
		return nil, errors.New(locales.AiModelMissing())
	}
	return ai.NewOpenAIProvider(ai.OpenAIConfig{
		BaseURL: base,
		APIKey:  a.ai.Prefs.APIKey,
	}), nil
}

// aiSnapshot 组装一次请求的上下文；默认只带 schema 与当前 SQL。
func (a *root) aiSnapshot() ai.ContextSnapshot {
	return a.aiSnapshotWith(a.aiEgressPolicy())
}

// aiSnapshotWith 按给定的一次性策略组装基础快照。
func (a *root) aiSnapshotWith(opts aiEgressPolicy) ai.ContextSnapshot {
	var snap ai.ContextSnapshot
	snap.Language = uiLocale()
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
	if connID != "" {
		key, schema := a.completionScope(connID)
		var tables, views []string
		if a.session.Catalog != nil {
			tables, views, _ = a.session.Catalog.objects(key)
		}
		// 补全目录没加载时退回对象树/元数据缓存，AI 不再拿到空 schema。
		if len(tables)+len(views) == 0 {
			if m := a.tree.Data[objectTreeKey(connID, schema)]; m != nil {
				for _, o := range m[engine.ObjectTable] {
					tables = append(tables, o.Name)
				}
				for _, o := range m[engine.ObjectView] {
					views = append(views, o.Name)
				}
			}
		}
		const maxTables = 200
		seen := map[string]bool{}
		for _, group := range [][]string{tables, views} {
			for _, name := range group {
				if len(snap.Tables) >= maxTables || name == "" || seen[strings.ToLower(name)] {
					continue
				}
				seen[strings.ToLower(name)] = true
				t := ai.TableContext{Name: name, Schema: schema}
				if a.session.Catalog != nil {
					if cols, ok := a.session.Catalog.columns(key, name); ok {
						for i, c := range cols {
							if i >= aiHydrateColumnsMax {
								break
							}
							t.Columns = append(t.Columns, ai.ColumnContext{
								Name: c.Name, Type: c.Type,
								Nullable: c.Nullable, PrimaryKey: c.PrimaryKey,
							})
							if c.PrimaryKey {
								t.PrimaryKey = append(t.PrimaryKey, c.Name)
							}
						}
					}
				}
				snap.Tables = append(snap.Tables, t)
			}
		}
	}
	if opts.history && a.history != nil && connID != "" {
		for _, h := range a.history.List(connID, 20) {
			snap.History = append(snap.History, ai.HistoryItem{
				SQL: ai.Redact(h.SQL), OK: h.OK, Err: ai.Redact(h.Err), Rows: h.Rows,
			})
		}
	}
	if opts.errors && a.consoleErr != "" {
		snap.Errors = append(snap.Errors, ai.ErrorItem{SQL: ai.Redact(snap.ActiveSQL), Err: ai.Redact(a.consoleErr)})
	}
	return snap
}

// maxAIDialogueMessages 是随请求发送的最近对话消息数（约 4 轮）。
const maxAIDialogueMessages = 8

// aiDialogue 把面板里已完成的消息转成模型对话历史，只保留最近几轮。
// 正在流式生成或没有正文的消息不发送。
func (a *root) aiDialogue() []ai.Message {
	_, msgs := a.aiDialogueParts()
	return msgs
}

// aiDialogueParts 把长对话拆成“更早的确定性摘要 + 最近 8 条消息”。
func (a *root) aiDialogueParts() (string, []ai.Message) {
	var all []ai.Message
	for _, m := range a.ai.Chat.messages {
		if strings.TrimSpace(m.text) == "" || !m.done {
			continue
		}
		switch m.role {
		case "user":
			all = append(all, ai.Message{Role: ai.RoleUser, Content: m.text})
		case "assistant":
			all = append(all, ai.Message{Role: ai.RoleAssistant, Content: m.text})
		}
	}
	if len(all) <= maxAIDialogueMessages {
		return "", all
	}
	older := all[:len(all)-maxAIDialogueMessages]
	var asks []string
	for _, m := range older {
		if m.Role != ai.RoleUser {
			continue
		}
		q := strings.Join(strings.Fields(m.Content), " ")
		asks = append(asks, truncateRunes(q, 80))
		if len(asks) >= 3 {
			break
		}
	}
	summary := locales.AiDialogueSummary(len(older), strings.Join(asks, " / "))
	return summary, all[len(all)-maxAIDialogueMessages:]
}

// chatSnapshot 是 Chat/预览专用快照：在普通上下文上附最近对话。
func (a *root) chatSnapshot() ai.ContextSnapshot {
	return a.chatSnapshotWith(a.aiEgressPolicy())
}

// chatSnapshotWith 在基础快照上附对话、结果、计划与缓存 DDL。
func (a *root) chatSnapshotWith(opts aiEgressPolicy) ai.ContextSnapshot {
	snap := a.aiSnapshotWith(opts)
	snap.DialogueSummary, snap.Dialogue = a.aiDialogueParts()
	snap.Result = a.aiResultContextWith(opts.samples)
	snap.Plan = a.aiPlanContext()
	if connID := a.activeConnID(); connID != "" {
		a.attachCachedAIRelations(&snap, connID)
		if opts.ddl {
			reqs := a.aiDDLRequests(connID, snap)
			a.attachAIDDL(&snap, connID, reqs)
		}
	}
	return snap
}

// sendAI 发送当前输入；服务端错误只写面板，不影响查询。
func (a *root) sendAI() {
	q := strings.TrimSpace(a.ai.Chat.input)
	if q == "" || a.ai.Chat.streaming || a.ai.Chat.preparing {
		return
	}
	policy := a.aiEgressPolicy()
	snap := a.chatSnapshotWith(policy)
	a.prepareAIContext(q, snap, policy.ddl, func(snap ai.ContextSnapshot) { a.sendAIChat(q, snap) })
}

// sendAIChat 在上下文准备好后组装提示词并开始流式请求。
func (a *root) sendAIChat(q string, snap ai.ContextSnapshot) {
	msgs, rep := ai.BuildChat(snap, q, a.aiBudget("chat", a.aiTaskMaxTokens("chat")))
	a.ai.Chat.lastCtx = aiContextNote(rep, "")
	a.ai.Chat.lint = aiSQLReport{}
	if err := a.startAIRequest(locales.AiKindChat(), aiContextNote(rep, ""), q, msgs); err != nil {
		a.ai.Chat.err = err.Error()
		return
	}
	a.ai.Chat.input = ""
}

// startAIRequest 追加一轮消息并发起流式请求；kind 只用于「最近 AI 请求」查看器。
func (a *root) startAIRequest(kind, note, display string, msgs []ai.Message) error {
	if a.ai.Chat.streaming {
		return errors.New(locales.AiAlreadyStreaming())
	}
	if a.ai.Chat.convID == "" {
		a.newAIConversation()
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		return err
	}
	model := a.aiTaskModel("chat")
	a.recordAIRequest(kind, model, note, msgs)
	a.ai.Chat.err = ""
	a.ai.Chat.usage = ai.Usage{}
	a.ai.Chat.usageCounted = false
	a.ai.Chat.estimate = 0
	for _, m := range msgs {
		a.ai.Chat.estimate += ai.EstimateTokens(m.Content)
	}
	a.ai.Chat.reqModel = model
	a.ai.Chat.messages = append(a.ai.Chat.messages,
		aiChatMessage{role: "user", text: display, done: true},
		aiChatMessage{role: "assistant"},
	)
	a.saveAIChat()
	if a.ai.Prefs.AITitle && len(a.ai.Chat.messages) == 2 {
		a.generateAITitle(a.ai.Chat.convID, display)
	}
	a.ai.Chat.seq++
	seq := a.ai.Chat.seq
	ctx, cancel := context.WithCancel(context.Background())
	a.ai.Chat.cancel = cancel
	a.ai.Chat.streaming = true
	a.ai.Chat.started = time.Now()
	a.ai.Chat.scroll.Y = math.MaxFloat32
	req := ai.Request{
		Model:       model,
		Messages:    msgs,
		Temperature: a.aiChatTemperature(),
		MaxTokens:   a.aiTaskMaxTokens("chat"),
	}
	if a.win == nil {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			a.ai.Chat.streaming = false
			a.ai.Chat.cancel = nil
			cancel()
			a.ai.Chat.err = err.Error()
			app.LogWarn("ai", "stream failed", "model", model, "err", err.Error())
			a.applyAIEvent(seq, ai.Event{Kind: ai.EventError, Err: err})
			return err
		}
		app.LogInfo("ai", "chat request", "model", model, "messages", len(msgs))
		a.consumeAI(seq, ch)
		return nil
	}
	win := a.win
	go func() {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			if ctx.Err() == nil {
				app.LogWarn("ai", "stream failed", "model", model, "err", err.Error())
				a.safeUpdate(win, func() { a.applyAIEvent(seq, ai.Event{Kind: ai.EventError, Err: err}) })
			}
			return
		}
		app.LogInfo("ai", "chat request", "model", model, "messages", len(msgs))
		a.consumeAI(seq, ch)
	}()
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
			a.safeUpdate(win, func() { a.applyAIEvent(seq, ai.Event{Kind: ai.EventText, Text: text}) })
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
				a.safeUpdate(win, func() { a.applyAIEvent(seq, event) })
			case <-ticker.C:
				flush()
			}
		}
	}()
}

// applyAIEvent 应用一个流事件；旧请求直接忽略。
func (a *root) applyAIEvent(seq uint64, ev ai.Event) {
	if seq != a.ai.Chat.seq {
		return
	}
	n := len(a.ai.Chat.messages)
	if n == 0 || a.ai.Chat.messages[n-1].role != "assistant" {
		return
	}
	msg := &a.ai.Chat.messages[n-1]
	switch ev.Kind {
	case ai.EventText:
		msg.text += ev.Text
	case ai.EventUsage:
		if ev.Usage != nil {
			a.ai.Chat.usage = *ev.Usage
			a.updateAITokenScale(a.ai.Chat.reqModel, a.ai.Chat.estimate, ev.Usage.PromptTokens)
		}
	case ai.EventDone:
		a.addAIUsage()
		msg.done = true
		a.ai.Chat.streaming = false
		a.ai.Chat.cancel = nil
		a.saveAIChat()
		app.LogInfo("ai", "chat done", "dur_ms", time.Since(a.ai.Chat.started).Milliseconds(),
			"tokens", a.ai.Chat.usage.TotalTokens)
	case ai.EventError:
		a.addAIUsage()
		if ev.Err != nil {
			msg.err = ev.Err.Error()
			a.ai.Chat.err = ev.Err.Error()
			app.LogWarn("ai", "chat error", "err", ev.Err.Error())
		}
		msg.done = true
		a.ai.Chat.streaming = false
		a.ai.Chat.cancel = nil
		a.saveAIChat()
	}
}

// updateAITokenScale 用 provider 实际 prompt tokens 校准估算倍率。
func (a *root) updateAITokenScale(model string, estimate, actual int) {
	if model == "" || estimate <= 0 || actual <= 0 {
		return
	}
	ratio := float64(actual) / float64(estimate)
	if ratio < 1 {
		ratio = 1
	}
	if ratio > 2 {
		ratio = 2
	}
	if a.ai.TokenScale == nil {
		a.ai.TokenScale = map[string]float64{}
	}
	old := a.ai.TokenScale[model]
	if old <= 0 {
		old = 1
	}
	next := old*0.7 + ratio*0.3
	if next < 1 {
		next = 1
	}
	if next > 2 {
		next = 2
	}
	a.ai.TokenScale[model] = next
}

// addAIUsage 把本次请求的 token 计入会话累计，只计一次。
func (a *root) addAIUsage() {
	if a.ai.Chat.usageCounted || a.ai.Chat.usage.TotalTokens == 0 {
		return
	}
	a.ai.Chat.usageCounted = true
	a.ai.Chat.totalUsage.PromptTokens += a.ai.Chat.usage.PromptTokens
	a.ai.Chat.totalUsage.CompletionTokens += a.ai.Chat.usage.CompletionTokens
	a.ai.Chat.totalUsage.TotalTokens += a.ai.Chat.usage.TotalTokens
}

// stopAI 取消当前请求并丢弃后续回包。
func (a *root) stopAI() {
	a.cancelAIPrepare()
	if !a.ai.Chat.streaming {
		return
	}
	a.ai.Chat.seq++
	if a.ai.Chat.cancel != nil {
		a.ai.Chat.cancel()
		a.ai.Chat.cancel = nil
	}
	a.ai.Chat.streaming = false
	if n := len(a.ai.Chat.messages); n > 0 {
		msg := &a.ai.Chat.messages[n-1]
		if msg.role == "assistant" && !msg.done {
			msg.done = true
			msg.err = locales.AiStopped()
		}
	}
	a.saveAIChat()
}

// cancelAIPrepare 作废正在补齐 DDL 的上下文准备。
func (a *root) cancelAIPrepare() {
	if !a.ai.Chat.preparing {
		return
	}
	a.ai.Chat.prepareSeq++
	a.ai.Chat.preparing = false
}

// resetAIChat 新建一段会话。
func (a *root) resetAIChat() {
	a.ai.Chat.showHistory = false
	a.newAIConversation()
}

// insertAISQL 把消息里第一个 SQL 代码块插入编辑器。
func (a *root) insertAISQL(text string) bool {
	sql, ok := ai.ExtractSQL(text)
	if !ok {
		return false
	}
	a.ai.Chat.lint = a.aiSQLCheck(sql)
	a.code.Insert(sql)
	return true
}

// aiChatFile 返回当前工作区的 AI 会话文件；未配置或没有工作区时为空。
func (a *root) aiChatFile() string {
	if a.ai.PrefsPath == "" {
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
	return app.AIConversationPath(filepath.Dir(a.ai.PrefsPath), ws.ID)
}

// ensureAIChatStore 保证内存里是当前工作区的会话库；切换工作区后重新读盘。
// 返回 false 表示没有可落盘的位置（测试或未配置）。
func (a *root) ensureAIChatStore() bool {
	path := a.aiChatFile()
	if path == "" {
		return false
	}
	if a.ai.Chat.storeLoaded && a.ai.Chat.storePath == path {
		return true
	}
	if a.ai.Chat.storeLoaded && a.ai.Chat.storePath != "" {
		// 旧工作区还有未落盘的消息：先写回旧文件，再切到新工作区。
		a.syncAIChat()
		if err := app.SaveAIConversations(a.ai.Chat.storePath, a.ai.Chat.store); err != nil {
			a.ai.Warn = locales.AiSaveConversationFailed(err.Error())
		}
	}
	store, warn := app.LoadAIConversations(path)
	if warn != "" {
		a.ai.Warn = warn
	}
	a.ai.Chat.store = store
	a.ai.Chat.storeLoaded = true
	a.ai.Chat.storePath = path
	return true
}

// loadAIChat 读取当前工作区会话（打开面板时调用）。
func (a *root) loadAIChat() {
	if !a.ensureAIChatStore() {
		a.ai.Chat.convID = ""
		a.newAIConversation()
		return
	}
	store := a.ai.Chat.store
	id := store.ActiveID
	if id == "" && len(store.Conversations) > 0 {
		id = store.Conversations[0].ID
	}
	if id == "" {
		a.ai.Chat.convID = ""
		a.newAIConversation()
		return
	}
	a.selectAIConversation(id, false)
}

// selectAIConversation 切换当前会话；saveCurrent 先把当前消息写回 store。
func (a *root) selectAIConversation(id string, saveCurrent bool) {
	a.stopAI()
	if saveCurrent {
		a.syncAIChat()
	}
	var conv *app.AIConversation
	for i := range a.ai.Chat.store.Conversations {
		if a.ai.Chat.store.Conversations[i].ID == id {
			conv = &a.ai.Chat.store.Conversations[i]
			break
		}
	}
	if conv == nil {
		return
	}
	a.ai.Chat.convID = id
	a.ai.Chat.messages = a.ai.Chat.messages[:0]
	a.ai.Chat.lint = aiSQLReport{}
	for _, m := range conv.Messages {
		a.ai.Chat.messages = append(a.ai.Chat.messages, aiChatMessage{
			role: m.Role, text: m.Text, done: m.Done, err: m.Err,
		})
	}
	a.ai.Chat.store.ActiveID = id
	a.ai.Chat.usage, a.ai.Chat.totalUsage = ai.Usage{}, ai.Usage{}
	a.ai.Chat.usageCounted = false
	a.ai.Chat.scroll.Y = math.MaxFloat32
	a.saveAIChat()
}

// newAIConversation 新建会话并切换过去。
func (a *root) newAIConversation() {
	a.stopAI()
	a.syncAIChat()
	now := time.Now().UnixMilli()
	conv := app.AIConversation{ID: app.NewAIConversationID(), CreatedAt: now, UpdatedAt: now}
	a.ai.Chat.store.Conversations = append(a.ai.Chat.store.Conversations, conv)
	a.ai.Chat.store.ActiveID = conv.ID
	a.ai.Chat.convID = conv.ID
	a.ai.Chat.messages = nil
	a.ai.Chat.err = ""
	a.ai.Chat.lastCtx = ""
	a.ai.Chat.lint = aiSQLReport{}
	a.ai.Chat.usage, a.ai.Chat.totalUsage = ai.Usage{}, ai.Usage{}
	a.ai.Chat.usageCounted = false
	a.ai.Chat.scroll.Y = 0
	a.saveAIChat()
}

// syncAIChat 把当前消息写回内存 store，并更新标题/时间。
func (a *root) syncAIChat() {
	if a.ai.Chat.convID == "" {
		return
	}
	idx := -1
	for i := range a.ai.Chat.store.Conversations {
		if a.ai.Chat.store.Conversations[i].ID == a.ai.Chat.convID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	conv := &a.ai.Chat.store.Conversations[idx]
	next := make([]app.AIMessageRecord, 0, len(a.ai.Chat.messages))
	for _, m := range a.ai.Chat.messages {
		next = append(next, app.AIMessageRecord{
			Role: m.role, Text: m.text, Done: m.done, Err: m.err,
		})
	}
	changed := !sameAIMessageList(conv.Messages, next)
	conv.Messages = next
	if !conv.TitleLocked {
		if title := app.AIConversationTitle(conv.Messages); title != "" {
			conv.Title = title
		}
	}
	if changed {
		conv.UpdatedAt = time.Now().UnixMilli()
		a.ai.Chat.historyLimit = 0
	}
	a.ai.Chat.store.ActiveID = conv.ID
}

// sameAIMessageList 比较两条消息列表，切换会话时避免刷新 UpdatedAt 导致排序跳动。
func sameAIMessageList(a, b []app.AIMessageRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Text != b[i].Text ||
			a[i].Done != b[i].Done || a[i].Err != b[i].Err {
			return false
		}
	}
	return true
}

// saveAIChat 保存当前工作区全部会话。
func (a *root) saveAIChat() {
	path := a.aiChatFile()
	if path == "" {
		return
	}
	a.syncAIChat()
	if err := app.SaveAIConversations(path, a.ai.Chat.store); err != nil {
		a.ai.Warn = locales.AiSaveConversationFailed(err.Error())
	}
}

// generateAITitle 在用户显式开启时用模型生成会话标题；失败静默保留消息截取标题。
func (a *root) generateAITitle(convID, firstMessage string) {
	if convID == "" {
		return
	}
	provider, err := a.aiProviderFor()
	if err != nil {
		return
	}
	msgs := []ai.Message{
		{Role: ai.RoleSystem, Content: locales.AiTitlePrompt()},
		{Role: ai.RoleUser, Content: firstMessage},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	apply := func(title string) {
		title = strings.TrimSpace(title)
		title = strings.Trim(title, "\"'“”「」")
		if runes := []rune(title); len(runes) > 20 {
			title = string(runes[:20])
		}
		if title == "" {
			return
		}
		for i := range a.ai.Chat.store.Conversations {
			if a.ai.Chat.store.Conversations[i].ID == convID {
				a.ai.Chat.store.Conversations[i].Title = title
				a.ai.Chat.store.Conversations[i].TitleLocked = true
				a.ai.Chat.store.Conversations[i].UpdatedAt = time.Now().UnixMilli()
				break
			}
		}
		a.saveAIChat()
	}
	read := func(ch <-chan ai.Event) string {
		var b strings.Builder
		for ev := range ch {
			if ev.Kind == ai.EventText {
				b.WriteString(ev.Text)
			}
		}
		return b.String()
	}
	req := ai.Request{Model: a.aiTaskModel("chat"), Messages: msgs, Temperature: 0, MaxTokens: 48}
	if a.win == nil {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err == nil {
			apply(read(ch))
		}
		return
	}
	win := a.win
	go func() {
		ch, err := openAIStream(ctx, cancel, provider, req)
		if err != nil {
			return
		}
		title := read(ch)
		a.safeUpdate(win, func() { apply(title) })
	}()
}

// saveAIPrefs 写盘 AI 配置；空路径（测试）不落盘。
func (a *root) saveAIPrefs() {
	if a.ai.PrefsPath == "" {
		return
	}
	if err := app.SaveAIPrefs(a.ai.PrefsPath, a.ai.Vault, a.ai.Prefs); err != nil {
		a.ai.Warn = locales.AiSaveConfigFailed(err.Error())
		return
	}
	a.ai.Warn = ""
}
