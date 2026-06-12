package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openaistream "github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAIChatProvider 使用 OpenAI Go SDK 的 Chat Completions API 实现 Provider。
type OpenAIChatProvider struct {
	cfg    ProviderConfig
	client openai.Client
}

// NewOpenAIChatProvider 创建 OpenAI Chat Completions Provider。
func NewOpenAIChatProvider(cfg ProviderConfig, httpClient *http.Client) *OpenAIChatProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenAIChatProvider{
		cfg: cfg,
		client: openai.NewClient(
			option.WithAPIKey(cfg.APIKey),
			option.WithBaseURL(cfg.BaseURL),
			option.WithHTTPClient(httpClient),
		),
	}
}

// CreateChatCompletion 实现 Provider 接口的非流式调用。
func (p *OpenAIChatProvider) CreateChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: toOpenAIChatMessages(req.Messages),
		Tools:    toOpenAIChatTools(req.Tools),
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai chat call failed: %w", err)
	}

	return fromOpenAIChatCompletion(resp), nil
}

// CreateChatCompletionStream 实现 Provider 接口的流式调用。
func (p *OpenAIChatProvider) CreateChatCompletionStream(ctx context.Context, req *ChatRequest, cb StreamCallback) error {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: toOpenAIChatMessages(req.Messages),
		Tools:    toOpenAIChatTools(req.Tools),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		},
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	return p.forwardStream(ctx, stream, cb)
}

// forwardStream 将 OpenAI Chat chunk 流转换为 StreamEvent 回调。
func (p *OpenAIChatProvider) forwardStream(ctx context.Context, stream *openaistream.Stream[openai.ChatCompletionChunk], cb StreamCallback) error {
	for stream.Next() {
		chunk := stream.Current()

		events := openaiChunkToStreamEvents(chunk)
		for _, evt := range events {
			cb(evt)
			if evt.Type == StreamEventDone || evt.Type == StreamEventError {
				if evt.Type == StreamEventError {
					return evt.Err
				}
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	if err := stream.Err(); err != nil {
		cb(StreamEvent{Type: StreamEventError, Err: fmt.Errorf("读取 OpenAI Chat 流失败: %w", err)})
		return err
	}

	return nil
}
