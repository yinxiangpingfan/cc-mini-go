package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	openaistream "github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAIResponsesProvider 使用 OpenAI Go SDK 的 Responses API 实现 Provider。
type OpenAIResponsesProvider struct {
	cfg    ProviderConfig
	client openai.Client
}

// NewOpenAIResponsesProvider 创建 OpenAI Responses Provider。
func NewOpenAIResponsesProvider(cfg ProviderConfig, httpClient *http.Client) *OpenAIResponsesProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &OpenAIResponsesProvider{
		cfg: cfg,
		client: openai.NewClient(
			option.WithAPIKey(cfg.APIKey),
			option.WithBaseURL(cfg.BaseURL),
			option.WithHTTPClient(httpClient),
		),
	}
}

// CreateChatCompletion 实现 Provider 接口的非流式调用。
func (p *OpenAIResponsesProvider) CreateChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: toResponsesInput(req.Messages),
		Tools: toResponsesTools(req.Tools),
	}

	// 系统提示通过 Instructions 字段传递
	if req.System != "" {
		params.Instructions = param.NewOpt(req.System)
	}

	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai responses call failed: %w", err)
	}

	return fromResponsesResponse(resp), nil
}

// CreateChatCompletionStream 实现 Provider 接口的流式调用。
func (p *OpenAIResponsesProvider) CreateChatCompletionStream(ctx context.Context, req *ChatRequest, cb StreamCallback) error {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: toResponsesInput(req.Messages),
		Tools: toResponsesTools(req.Tools),
	}

	if req.System != "" {
		params.Instructions = param.NewOpt(req.System)
	}

	stream := p.client.Responses.NewStreaming(ctx, params)
	return p.forwardStream(ctx, stream, cb)
}

// forwardStream 将 Responses 流式事件转换为 StreamEvent 回调。
func (p *OpenAIResponsesProvider) forwardStream(ctx context.Context, stream *openaistream.Stream[responses.ResponseStreamEventUnion], cb StreamCallback) error {
	for stream.Next() {
		evt := stream.Current()

		events := fromResponsesEvent(evt)
		for _, e := range events {
			cb(e)
			if e.Type == StreamEventDone || e.Type == StreamEventError {
				if e.Type == StreamEventError {
					return e.Err
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
		cb(StreamEvent{Type: StreamEventError, Err: fmt.Errorf("读取 OpenAI Responses 流失败: %w", err)})
		return err
	}

	return nil
}
