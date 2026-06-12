package provider

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	anthropicstream "github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

const defaultHTTPTimeout = 2 * time.Minute

// AnthropicProvider 使用 Anthropic Go SDK 实现 Provider 接口。
type AnthropicProvider struct {
	cfg    ProviderConfig
	client anthropic.Client
}

// ProviderConfig 包含创建 Provider 所需的配置。
type ProviderConfig struct {
	BaseURL  string
	APIKey   string
	Model    string
	Thinking *ThinkingConfig
}

// ThinkingConfig extended thinking 配置。
type ThinkingConfig struct {
	Enabled      bool
	BudgetTokens int64
}

// NewAnthropicProvider 创建 Anthropic Provider。
func NewAnthropicProvider(cfg ProviderConfig, httpClient *http.Client) *AnthropicProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &AnthropicProvider{
		cfg: cfg,
		client: anthropic.NewClient(
			option.WithAPIKey(cfg.APIKey),
			option.WithBaseURL(cfg.BaseURL),
			option.WithHTTPClient(httpClient),
		),
	}
}

// CreateChatCompletion 实现 Provider 接口的非流式调用。
func (p *AnthropicProvider) CreateChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := p.buildParams(model, req)

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic call failed: %w", err)
	}

	respMsg := fromAnthropicMessage(msg)
	finish := finishReasonFromAnthropic(msg.StopReason)

	return &ChatResponse{
		Message:      respMsg,
		Usage:        fromAnthropicUsage(msg.Usage),
		FinishReason: finish,
	}, nil
}

// CreateChatCompletionStream 实现 Provider 接口的流式调用。
func (p *AnthropicProvider) CreateChatCompletionStream(ctx context.Context, req *ChatRequest, cb StreamCallback) error {
	model := modelOrDefault(req.Model, p.cfg.Model)
	params := p.buildParams(model, req)

	stream := p.client.Messages.NewStreaming(ctx, params)
	return p.forwardStream(ctx, stream, cb)
}

// buildParams 构造 Anthropic MessageNewParams。
func (p *AnthropicProvider) buildParams(model string, req *ChatRequest) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 4096,
		Messages:  toAnthropicMessages(req.Messages),
		Tools:     toAnthropicTools(req.Tools),
	}

	// 系统提示通过 System 字段传递
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	} else {
		// 从消息中提取 system 消息
		params.System = extractAnthropicSystem(req.Messages)
	}

	// 配置 extended thinking
	thinking := p.cfg.Thinking
	if thinking != nil && thinking.Enabled {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(thinking.BudgetTokens)
		params.MaxTokens = 8192 // thinking 需要更大的 max_tokens
	}

	return params
}

// forwardStream 将 Anthropic 流式事件转换为统一的 StreamEvent 回调。
func (p *AnthropicProvider) forwardStream(ctx context.Context, stream *anthropicstream.Stream[anthropic.MessageStreamEventUnion], cb StreamCallback) error {
	// 跟踪当前正在构建的工具调用
	var activeToolCalls map[int]*toolCallBuilder
	defer func() { activeToolCalls = nil }()

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			// 检测新的工具调用开始
			if block := e.ContentBlock.AsAny(); block != nil {
				if toolUse, ok := block.(anthropic.ToolUseBlock); ok {
					idx := int(e.Index)
					if activeToolCalls == nil {
						activeToolCalls = make(map[int]*toolCallBuilder)
					}
					activeToolCalls[idx] = &toolCallBuilder{
						id:   toolUse.ID,
						name: toolUse.Name,
					}
					cb(StreamEvent{
						Type:          StreamEventToolCallBegin,
						ToolCallIndex: idx,
						ToolCallID:    toolUse.ID,
						ToolCallName:  toolUse.Name,
					})
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			// 处理增量 delta
			switch delta := e.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if delta.Text != "" {
					cb(StreamEvent{Type: StreamEventContentDelta, Delta: delta.Text})
				}
			case anthropic.InputJSONDelta:
				if delta.PartialJSON != "" {
					cb(StreamEvent{
						Type:          StreamEventToolCallDelta,
						ToolCallIndex: int(e.Index),
						Delta:         delta.PartialJSON,
					})
				}
			case anthropic.ThinkingDelta:
				if delta.Thinking != "" {
					cb(StreamEvent{Type: StreamEventThinkingDelta, Delta: delta.Thinking})
				}
			}

		case anthropic.MessageDeltaEvent:
			// 包含 usage 信息
			usage := fromAnthropicUsage(anthropic.Usage{
				InputTokens:  e.Usage.InputTokens,
				OutputTokens: e.Usage.OutputTokens,
			})
			_ = usage // usage 在 stop 事件中处理

		case anthropic.MessageStopEvent:
			cb(StreamEvent{Type: StreamEventDone})
			activeToolCalls = nil
			return nil
		}

		// 检查 context 取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	if err := stream.Err(); err != nil {
		cb(StreamEvent{Type: StreamEventError, Err: fmt.Errorf("读取 Anthropic 流失败: %w", err)})
		return err
	}

	return nil
}

// toolCallBuilder 流式构建中的工具调用信息。
type toolCallBuilder struct {
	id   string
	name string
}

// modelOrDefault 模型名回退。
func modelOrDefault(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
