package ui

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"db-g/ai"
)

// aiPanel 是右侧 AI 助手面板。
func (a *root) aiPanel(c *ui.Context) {
	st := &a.aiChat
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Padding(8).Gap(6).Label("AI 面板").Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			iconChild(c, iconSparkles, t.Accent)
			ui.Text(c, "AI 助手").Bold()
			if a.aiPrefs.Model != "" {
				ui.Text(c, a.aiPrefs.Model).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
			ui.Spacer(c)
			if iconButtonPlain(c, iconPlus, "新会话", t.TextMuted).Clicked() {
				a.resetAIChat()
			}
			if iconButtonPlain(c, iconClose, "关闭 AI 面板", t.TextMuted).Clicked() {
				st.open = false
			}
		})
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
			Placeholder("问点什么。Enter 发送，Shift+Enter 换行").
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyEnter && k.Mods == 0 {
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
