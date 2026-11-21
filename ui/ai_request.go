package ui

import (
	"context"
	"errors"
	"time"

	"db-g/ai"
	"db-g/locales"
)

// aiIdleTimeout 是流式响应空闲多久后自动取消；正常生成不会长时间没有事件。
const aiIdleTimeout = 60 * time.Second

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
