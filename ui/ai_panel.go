package ui

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"db-g/ai"
	"db-g/app"
)

// aiPanel 是右侧 AI 助手面板。
func (a *root) aiPanel(c *ui.Context) {
	st := &a.aiChat
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Padding(8).Gap(6).Label("AI 面板").Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			iconChild(c, iconSparkles, t.Accent)
			title := a.currentAIConversationTitle()
			if title == "" {
				title = "AI 助手"
			}
			ui.Text(c, title).Bold().SingleLine()
			if a.aiPrefs.Model != "" {
				ui.Text(c, a.aiPrefs.Model).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
			ui.Spacer(c)
			if iconButtonPlain(c, iconHistory, "会话历史", t.TextMuted).Clicked() {
				st.showHistory = !st.showHistory
			}
			if iconButtonPlain(c, iconPlus, "新会话", t.TextMuted).Clicked() {
				a.resetAIChat()
			}
			if iconButtonPlain(c, iconClose, "关闭 AI 面板", t.TextMuted).Clicked() {
				st.open = false
			}
		})
		if st.showHistory {
			a.aiHistoryList(c)
		}
		if a.aiWarn != "" {
			ui.Text(c, a.aiWarn).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(a.aiWarn)
		}
		if st.scroll.Y >= st.scroll.MaxY {
			st.scroll.Y = math.MaxFloat32
		}
		ui.Scroll(c).TrackScroll(&st.scroll).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				if len(st.messages) == 0 {
					ui.Text(c, "问关于当前连接、表结构或 SQL 的问题。生成结果不会自动执行。").FontSize(12).
						TextColor(t.TextMuted)
				}
				for i := range st.messages {
					a.aiMessage(c, &st.messages[i])
				}
				if st.streaming {
					ui.Text(c, "生成中…").FontSize(11).TextColor(t.TextMuted)
				}
			})
		})
		if st.lastCtx != "" {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, st.lastCtx).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
				ui.Spacer(c)
				if ui.Button(c, "查看上下文").FontSize(10.5).Clicked() {
					st.ctxOpen = true
				}
			})
		}
		if st.err != "" {
			ui.Text(c, st.err).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(st.err)
		}
		ui.TextArea(c, &st.input).
			Lines(1, 6).
			Placeholder("问点什么。Enter / Ctrl+Enter 发送，Shift+Enter 换行").
			Label("AI 提问").
			Bind(&st.inputRef).
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && (k.Mods == 0 || k.Mods == ui.Ctrl) {
					a.sendAI()
					return true
				}
				return false
			})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if st.streaming {
				if ui.Button(c, "停止").Clicked() {
					a.stopAI()
				}
			} else if ui.PrimaryButton(c, "发送").Disabled(st.input == "").Clicked() {
				a.sendAI()
			}
			ui.Spacer(c)
			if st.usage.TotalTokens > 0 {
				ui.Text(c, "tokens "+strconv.Itoa(st.usage.TotalTokens)).FontSize(10.5).TextColor(t.TextMuted)
			}
		})
	})
}

func (a *root) aiContextModal(c *ui.Context) {
	if !a.aiChat.ctxOpen {
		return
	}
	ui.Modal(c, &a.aiChat.ctxOpen, func() {
		ui.Text(c, "将发送给模型的内容").Bold()
		ui.Text(c, "不包含 DSN、密码；历史/样本/错误日志只有开启后才会出现。").FontSize(11).
			TextColor(c.Theme().TextMuted)
		ui.TextArea(c, &a.aiChat.lastPrompt).ReadOnly(true).Lines(16, 24)
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				a.aiChat.ctxOpen = false
			}
		})
	}).Width(720).Height(560)
}

func (a *root) aiMessage(c *ui.Context, m *aiChatMessage) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		label := "AI"
		col := t.TextMuted
		if m.role == "user" {
			label = "我"
			col = t.Accent
		}
		ui.Text(c, label).FontSize(10.5).TextColor(col).Bold()
		if m.text != "" {
			if m.role == "assistant" {
				if m.done {
					markdown.View(c, m.text, &a.aiChat.md).FillWidth()
				} else {
					markdown.Stream(c, m.text, &a.aiChat.md).FillWidth()
				}
			} else {
				ui.Text(c, m.text).FontSize(12.5)
			}
		}
		if m.err != "" {
			ui.Text(c, m.err).FontSize(11).TextColor(t.Danger).SingleLine().Tooltip(m.err)
		}
		if m.done && m.role == "assistant" {
			if sql, ok := ai.ExtractSQL(m.text); ok {
				ui.Row(c).Gap(4).Children(func() {
					if ui.Button(c, "插入 SQL").Clicked() {
						a.code.Insert(sql)
					}
					if ui.Button(c, "复制 SQL").Clicked() {
						c.WriteClipboard(sql)
					}
				})
			}
		}
	})
}

// currentAIConversationTitle 返回当前会话的标题（空 = 新会话）。
func (a *root) currentAIConversationTitle() string {
	for _, conv := range a.aiChat.store.Conversations {
		if conv.ID == a.aiChat.convID {
			if conv.Title != "" {
				return conv.Title
			}
			return app.AIConversationTitle(conv.Messages)
		}
	}
	return ""
}

// aiHistoryList 是面板内的会话历史列表：按最近消息时间倒排，搜索 + 滚动分页。
func (a *root) aiHistoryList(c *ui.Context) {
	st := &a.aiChat
	t := c.Theme()
	convs := append([]app.AIConversation(nil), st.store.Conversations...)
	sort.SliceStable(convs, func(i, j int) bool {
		if convs[i].UpdatedAt != convs[j].UpdatedAt {
			return convs[i].UpdatedAt > convs[j].UpdatedAt
		}
		if convs[i].CreatedAt != convs[j].CreatedAt {
			return convs[i].CreatedAt > convs[j].CreatedAt
		}
		return convs[i].ID > convs[j].ID
	})
	titleOf := func(conv app.AIConversation) string {
		if conv.Title != "" {
			return conv.Title
		}
		if title := app.AIConversationTitle(conv.Messages); title != "" {
			return title
		}
		return "新会话"
	}
	if q := strings.ToLower(strings.TrimSpace(st.historyQuery)); q != "" {
		filtered := convs[:0]
		for _, conv := range convs {
			if strings.Contains(strings.ToLower(titleOf(conv)), q) {
				filtered = append(filtered, conv)
			}
		}
		convs = filtered
	}
	if st.historyLimit < 20 {
		st.historyLimit = 20
	}
	if st.historyScroll.MaxY > 0 && st.historyScroll.Y >= st.historyScroll.MaxY-24 &&
		st.historyLimit < len(convs) {
		st.historyLimit += 20
	}
	if st.historyLimit > len(convs) {
		st.historyLimit = len(convs)
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, "会话历史").FontSize(11).Bold().TextColor(t.TextMuted)
		if ui.TextInput(c, &st.historyQuery).Placeholder("搜索会话").FontSize(11.5).
			Radius(t.Space(1)).Changed() {
			st.historyLimit = 20
			st.historyScroll.Y = 0
		}
		ui.Scroll(c).TrackScroll(&st.historyScroll).FillWidth().Height(180).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				if len(convs) == 0 {
					ui.Text(c, "没有匹配的会话").FontSize(11).TextColor(t.TextMuted)
					return
				}
				limit := st.historyLimit
				if limit > len(convs) {
					limit = len(convs)
				}
				for _, conv := range convs[:limit] {
					label := titleOf(conv)
					btn := ui.ButtonBase(c).Label(label).Justify(ui.Start).Padding(3, 6).Radius(t.Space(1))
					if conv.ID == st.convID {
						btn.Background(t.Accent.Alpha(0.12))
					} else if btn.Hovered() {
						btn.Background(t.SurfaceHover)
					}
					btn.Children(func() {
						txt := ui.Text(c, label).FontSize(11.5).SingleLine()
						if conv.ID == st.convID {
							txt.Bold().TextColor(t.Accent)
						}
					})
					if btn.Clicked() {
						a.stopAI()
						a.selectAIConversation(conv.ID, true)
						st.showHistory = false
					}
				}
				if limit < len(convs) {
					ui.Text(c, "继续向下滚动加载更多…").FontSize(10.5).TextColor(t.TextMuted)
				}
			})
		})
	})
}
