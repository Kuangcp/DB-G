package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"db-g/ai"
	"db-g/app"
	"db-g/locales"
)

// aiIdleTimeout 是流式响应空闲多久后自动取消；正常生成不会长时间没有事件。
const aiIdleTimeout = 60 * time.Second

// aiTaskModel 返回任务模型；未单独配置时继承聊天模型。
func (a *root) aiTaskModel(task string) string {
	switch task {
	case "completion":
		if m := strings.TrimSpace(a.ai.Prefs.CompletionModel); m != "" {
			return m
		}
	case "fix":
		if m := strings.TrimSpace(a.ai.Prefs.FixModel); m != "" {
			return m
		}
	}
	if m := strings.TrimSpace(a.ai.Prefs.Model); m != "" {
		return m
	}
	return "local"
}

// aiTaskMaxTokens 返回任务输出上限；预算与请求共用同一个值。
func (a *root) aiTaskMaxTokens(task string) int {
	def := app.DefaultAIPrefs()
	value := func(v, fallback int) int {
		if v > 0 {
			return v
		}
		return fallback
	}
	switch task {
	case "completion":
		return value(a.ai.Prefs.CompletionMaxTokens, def.CompletionMaxTokens)
	case "fix":
		return value(a.ai.Prefs.FixMaxTokens, def.FixMaxTokens)
	case "rewrite":
		return value(a.ai.Prefs.RewriteMaxTokens, def.RewriteMaxTokens)
	}
	return value(a.ai.Prefs.ChatMaxTokens, def.ChatMaxTokens)
}

// aiChatTemperature 返回聊天温度；旧配置/未加载配置时回退默认 0.2。
func (a *root) aiChatTemperature() float32 {
	t := a.ai.Prefs.ChatTemperature
	if t < 0.1 || t > 2 {
		t = app.DefaultAIPrefs().ChatTemperature
	}
	return float32(t)
}

// openAIStream 建立流、失败时重试一次，并套上空闲看门狗。
func openAIStream(ctx context.Context, cancel context.CancelFunc, p ai.Provider, req ai.Request) (<-chan ai.Event, error) {
	ch, err := p.Stream(ctx, req)
	if err != nil && ctx.Err() == nil {
		ch, err = p.Stream(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	return watchAIStream(ctx, cancel, ch, aiIdleTimeout), nil
}

// watchAIStream 在 idle 内没有任何事件时取消请求并发出本地化超时错误。
func watchAIStream(ctx context.Context, cancel context.CancelFunc, ch <-chan ai.Event, idle time.Duration) <-chan ai.Event {
	out := make(chan ai.Event, 32)
	go func() {
		defer close(out)
		timer := time.NewTimer(idle)
		defer timer.Stop()
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-timer.C:
				select {
				case out <- ai.Event{Kind: ai.EventError, Err: errors.New(locales.AiTimeout())}:
				case <-ctx.Done():
				}
				cancel()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
