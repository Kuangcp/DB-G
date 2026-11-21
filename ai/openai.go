package ai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIConfig 是 OpenAI-compatible 服务的连接配置。
type OpenAIConfig struct {
	BaseURL string
	APIKey  string
}

// OpenAIProvider 用 go-openai SDK 访问 OpenAI-compatible 服务。
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider 创建 Provider；BaseURL 形如 http://127.0.0.1:11434/v1。
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	cc := openai.DefaultConfig(cfg.APIKey)
	if base := strings.TrimSpace(cfg.BaseURL); base != "" {
		cc.BaseURL = strings.TrimRight(base, "/")
	}
	// 流式请求不能用 Client.Timeout（会截断长回答），只限制建连/握手/响应头；
	// 流中长时间无事件由 ui 侧的空闲看门狗处理。
	cc.HTTPClient = &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}}
	return &OpenAIProvider{client: openai.NewClientWithConfig(cc)}
}

// Models 返回服务端可用模型；本地服务不支持时返回错误由 UI 展示。
func (p *OpenAIProvider) Models(ctx context.Context) ([]string, error) {
	resp, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Models))
	for _, m := range resp.Models {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Stream 发起一次流式聊天补全；事件流在 ctx 结束时关闭。
func (p *OpenAIProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	sdkReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
	}
	for _, m := range req.Messages {
		sdkReq.Messages = append(sdkReq.Messages, openai.ChatCompletionMessage{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	stream, err := p.client.CreateChatCompletionStream(ctx, sdkReq)
	if err != nil {
		return nil, err
	}
	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		emit := func(ev Event) bool {
			select {
			case ch <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			resp, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					emit(Event{Kind: EventDone})
				} else if ctx.Err() == nil {
					emit(Event{Kind: EventError, Err: err})
				}
				return
			}
			for _, choice := range resp.Choices {
				if delta := choice.Delta.Content; delta != "" {
					if !emit(Event{Kind: EventText, Text: delta}) {
						return
					}
				}
			}
			if resp.Usage != nil {
				emit(Event{Kind: EventUsage, Usage: &Usage{
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
					TotalTokens:      resp.Usage.TotalTokens,
				}})
			}
		}
	}()
	return ch, nil
}
