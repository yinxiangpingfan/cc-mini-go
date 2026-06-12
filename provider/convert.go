package provider

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicparam "github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/openai/openai-go/v3"
	oaiparam "github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// =============================================================================
// OpenAI Tool (规范格式) → Anthropic Tool
// =============================================================================

// toAnthropicTools 将规范格式的工具定义转换为 Anthropic SDK 格式。
func toAnthropicTools(tools []client.Tool) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		desc := t.Function.Description
		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Function.Name,
				Description: anthropicDesc(desc),
				InputSchema: anthropic.ToolInputSchemaParam{
					Type:       "object",
					Properties: t.Function.Parameters.Properties,
					Required:   t.Function.Parameters.Required,
				},
			},
		})
	}
	return result
}

// anthropicDesc wraps a string as an Anthropic param.Opt[string].
func anthropicDesc(s string) anthropicparam.Opt[string] {
	if s == "" {
		return anthropicparam.Opt[string]{}
	}
	return anthropicparam.NewOpt(s)
}

// oaiDesc wraps a string as an OpenAI param.Opt[string].
func oaiDesc(s string) oaiparam.Opt[string] {
	if s == "" {
		return oaiparam.Opt[string]{}
	}
	return oaiparam.NewOpt(s)
}

// =============================================================================
// 规范格式消息 → Anthropic 消息
// =============================================================================

// extractAnthropicSystem 从消息列表中提取所有 system 角色的消息内容，
// 拼接为 Anthropic 的 System 参数。
func extractAnthropicSystem(messages []any) []anthropic.TextBlockParam {
	var blocks []anthropic.TextBlockParam
	for _, m := range messages {
		msg, ok := m.(*client.Message)
		if !ok || msg.Role != "system" {
			continue
		}
		content := msgContentString(msg.Content)
		if content != "" {
			blocks = append(blocks, anthropic.TextBlockParam{Text: content})
		}
	}
	return blocks
}

// toAnthropicMessages 将规范格式消息转换为 Anthropic SDK 消息格式。
// system 消息由 extractAnthropicSystem 单独处理，不放入 messages 数组。
func toAnthropicMessages(messages []any) []anthropic.MessageParam {
	var result []anthropic.MessageParam

	for _, m := range messages {
		switch msg := m.(type) {
		case *client.Message:
			switch msg.Role {
			case "user":
				result = append(result, anthropic.NewUserMessage(
					anthropic.NewTextBlock(msgContentString(msg.Content)),
				))
			case "assistant":
				result = append(result, messageToAssistantBlock(msg))
				// system messages are handled by extractAnthropicSystem
			}

		case *client.ResponseMessage:
			// Assistant message with optional tool calls
			result = append(result, responseMsgToAnthropic(msg))

		case *client.ToolsMessage:
			// Tool result → user message with tool_result content block
			result = append(result, toolsMsgToAnthropic(msg))
		}
	}

	return mergeConsecutiveSameRole(result)
}

// messageToAssistantBlock 将纯文本 assistant message 转为 Anthropic 格式。
func messageToAssistantBlock(msg *client.Message) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(
		anthropic.NewTextBlock(msgContentString(msg.Content)),
	)
}

// responseMsgToAnthropic 将含 tool_calls 的 ResponseMessage 转为 Anthropic 格式。
func responseMsgToAnthropic(msg *client.ResponseMessage) anthropic.MessageParam {
	blocks := make([]anthropic.ContentBlockParamUnion, 0)

	// 文本内容
	if content := msgContentString(msg.Content); content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(content))
	}

	// 工具调用 → tool_use 块
	for _, tc := range msg.ToolCalls {
		var input any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = tc.Function.Arguments // fallback: raw string
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.Id, input, tc.Function.Name))
	}

	return anthropic.NewAssistantMessage(blocks...)
}

// toolsMsgToAnthropic 将 tool 消息转为 Anthropic 的 tool_result 块。
// tool_result 必须放在 user 消息中（Anthropic 协议要求）。
func toolsMsgToAnthropic(msg *client.ToolsMessage) anthropic.MessageParam {
	return anthropic.NewUserMessage(
		anthropic.NewToolResultBlock(msg.ToolsId, msg.Content, false),
	)
}

// mergeConsecutiveSameRole 合并连续同角色消息。
// Anthropic 要求 user 和 assistant 严格交替，连续的 tool_result
// 需要合并到同一个 user 消息中。
func mergeConsecutiveSameRole(messages []anthropic.MessageParam) []anthropic.MessageParam {
	if len(messages) <= 1 {
		return messages
	}

	var result []anthropic.MessageParam
	current := messages[0]

	for i := 1; i < len(messages); i++ {
		next := messages[i]
		if current.Role == next.Role && current.Role == anthropic.MessageParamRoleUser {
			// 合并两个 user 消息的内容块
			current.Content = append(current.Content, next.Content...)
		} else {
			result = append(result, current)
			current = next
		}
	}
	result = append(result, current)
	return result
}

// =============================================================================
// Anthropic 响应 → 规范格式
// =============================================================================

// fromAnthropicMessage 将 Anthropic Message 转换为规范格式的 ResponseMessage。
func fromAnthropicMessage(msg *anthropic.Message) *client.ResponseMessage {
	if msg == nil {
		return nil
	}

	respMsg := &client.ResponseMessage{
		Role: "assistant",
	}

	var textParts []string
	var toolCalls []client.ToolCall

	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			textParts = append(textParts, variant.Text)
		case anthropic.ToolUseBlock:
			toolCalls = append(toolCalls, client.ToolCall{
				Id:   variant.ID,
				Type: "function",
				Function: client.FunctionCall{
					Name:      variant.Name,
					Arguments: string(variant.Input),
				},
			})
		}
	}

	if len(textParts) > 0 {
		respMsg.Content = joinStrings(textParts, "\n")
	} else {
		respMsg.Content = nil // 纯工具调用时 content 为 null
	}
	respMsg.ToolCalls = toolCalls

	return respMsg
}

// finishReasonFromAnthropic 将 Anthropic 的 stop_reason 转为 OpenAI 风格的 finish_reason。
func finishReasonFromAnthropic(reason anthropic.StopReason) string {
	switch reason {
	case anthropic.StopReasonEndTurn:
		return "stop"
	case anthropic.StopReasonMaxTokens:
		return "length"
	case anthropic.StopReasonToolUse:
		return "tool_calls"
	case anthropic.StopReasonStopSequence:
		return "stop"
	case anthropic.StopReasonRefusal:
		return "content_filter"
	default:
		return "stop"
	}
}

// =============================================================================
// Anthropic Usage → 规范格式
// =============================================================================

func fromAnthropicUsage(u anthropic.Usage) *client.Usage {
	return &client.Usage{
		PromptTokens:     int(u.InputTokens),
		CompletionTokens: int(u.OutputTokens),
		TotalTokens:      int(u.InputTokens + u.OutputTokens),
	}
}

// =============================================================================
// 规范格式消息 → OpenAI Chat Completions 格式
// =============================================================================

// toOpenAIChatMessages 将规范格式消息转换为 OpenAI SDK ChatCompletion 消息格式。
func toOpenAIChatMessages(messages []any) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))

	for _, m := range messages {
		switch msg := m.(type) {
		case *client.Message:
			switch msg.Role {
			case "system":
				result = append(result, openai.SystemMessage(msgContentString(msg.Content)))
			case "user":
				result = append(result, openai.UserMessage(msgContentString(msg.Content)))
			case "assistant":
				result = append(result, openai.AssistantMessage(msgContentString(msg.Content)))
			}

		case *client.ResponseMessage:
			result = append(result, responseMsgToOpenAIChat(msg))

		case *client.ToolsMessage:
			result = append(result, openai.ToolMessage(msg.Content, msg.ToolsId))
		}
	}

	return result
}

// responseMsgToOpenAIChat 将 ResponseMessage 转为 OpenAI Chat 格式。
func responseMsgToOpenAIChat(msg *client.ResponseMessage) openai.ChatCompletionMessageParamUnion {
	contentStr := msgContentString(msg.Content)

	// 纯文本回复
	if len(msg.ToolCalls) == 0 {
		return openai.AssistantMessage(contentStr)
	}

	// 含工具调用的回复
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.Id,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			},
		})
	}

	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: oaiparam.NewOpt(contentStr),
			},
			ToolCalls: toolCalls,
		},
	}
}

// =============================================================================
// OpenAI Chat Completions 响应 → 规范格式
// =============================================================================

// fromOpenAIChatCompletion 将 OpenAI ChatCompletion 转为规范格式。
func fromOpenAIChatCompletion(resp *openai.ChatCompletion) *ChatResponse {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}

	choice := resp.Choices[0]
	msg := choice.Message

	// 转换 tool_calls
	var toolCalls []client.ToolCall
	for _, tc := range msg.ToolCalls {
		if fn := tc.AsFunction(); fn.ID != "" {
			toolCalls = append(toolCalls, client.ToolCall{
				Id:   fn.ID,
				Type: "function",
				Function: client.FunctionCall{
					Name:      fn.Function.Name,
					Arguments: fn.Function.Arguments,
				},
			})
		}
	}

	return &ChatResponse{
		Message: &client.ResponseMessage{
			Role:      "assistant",
			Content:   msg.Content,
			ToolCalls: toolCalls,
		},
		Usage:        fromOpenAIUsage(resp.Usage),
		FinishReason: choice.FinishReason,
	}
}

// =============================================================================
// OpenAI Chat Chunk → StreamEvent
// =============================================================================

// openaiChunkToStreamEvents 将 OpenAI ChatCompletionChunk 转换为 StreamEvent 列表。
// 一个 chunk 可能产生多个事件（如同时有 content 和 tool_calls）。
func openaiChunkToStreamEvents(chunk openai.ChatCompletionChunk) []StreamEvent {
	var events []StreamEvent

	for _, choice := range chunk.Choices {
		// 文本增量
		if choice.Delta.Content != "" {
			events = append(events, StreamEvent{
				Type:  StreamEventContentDelta,
				Delta: choice.Delta.Content,
			})
		}

		// 工具调用增量
		for _, tc := range choice.Delta.ToolCalls {
			idx := int(tc.Index)
			// 工具调用开始（有 ID 表明是新工具调用）
			if tc.ID != "" {
				events = append(events, StreamEvent{
					Type:          StreamEventToolCallBegin,
					ToolCallIndex: idx,
					ToolCallID:    tc.ID,
					ToolCallName:  tc.Function.Name,
				})
			}
			// 工具参数增量
			if tc.Function.Arguments != "" {
				events = append(events, StreamEvent{
					Type:          StreamEventToolCallDelta,
					ToolCallIndex: idx,
					Delta:         tc.Function.Arguments,
				})
			}
		}

		// 完成
		if choice.FinishReason != "" {
			events = append(events, StreamEvent{
				Type:  StreamEventDone,
				Usage: fromOpenAIUsage(chunk.Usage),
			})
		}
	}

	return events
}

// =============================================================================
// OpenAI Usage → 规范格式
// =============================================================================

func fromOpenAIUsage(u openai.CompletionUsage) *client.Usage {
	if u.TotalTokens == 0 && u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return nil
	}
	return &client.Usage{
		PromptTokens:     int(u.PromptTokens),
		CompletionTokens: int(u.CompletionTokens),
		TotalTokens:      int(u.TotalTokens),
	}
}

// =============================================================================
// 规范格式消息 → OpenAI Responses API 输入
// =============================================================================

// toResponsesInput 将规范格式消息转换为 OpenAI Responses API 输入。
func toResponsesInput(messages []any) responses.ResponseNewParamsInputUnion {
	if len(messages) == 0 {
		return responses.ResponseNewParamsInputUnion{
			OfString: oaiparam.NewOpt(""),
		}
	}

	items := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, m := range messages {
		switch msg := m.(type) {
		case *client.Message:
			items = append(items, responses.ResponseInputItemUnionParam{
				OfMessage: &responses.EasyInputMessageParam{
					Role: toResponsesRole(msg.Role),
					Content: responses.EasyInputMessageContentUnionParam{
						OfString: oaiparam.NewOpt(msgContentString(msg.Content)),
					},
				},
			})

		case *client.ResponseMessage:
			// Assistant 消息（含可能的 tool_calls）
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					items = append(items, responses.ResponseInputItemUnionParam{
						OfFunctionCall: &responses.ResponseFunctionToolCallParam{
							CallID:    tc.Id,
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}
			}
			items = append(items, responses.ResponseInputItemUnionParam{
				OfMessage: &responses.EasyInputMessageParam{
					Role: responses.EasyInputMessageRoleAssistant,
					Content: responses.EasyInputMessageContentUnionParam{
						OfString: oaiparam.NewOpt(msgContentString(msg.Content)),
					},
				},
			})

		case *client.ToolsMessage:
			// 使用 function_call_output 格式
			items = append(items, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: msg.ToolsId,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: oaiparam.NewOpt(msg.Content),
					},
				},
			})
		}
	}

	return responses.ResponseNewParamsInputUnion{
		OfInputItemList: responses.ResponseInputParam(items),
	}
}

// toResponsesTools 将规范格式工具转为 Responses API 格式。
func toResponsesTools(tools []client.Tool) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		result = append(result, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        t.Function.Name,
				Description: oaiparam.NewOpt(t.Function.Description),
				Parameters:  t.Function.Parameters.Properties,
			},
		})
	}
	return result
}

// fromResponsesEvent 将 Responses 流式事件转为 StreamEvent 列表。
func fromResponsesEvent(evt responses.ResponseStreamEventUnion) []StreamEvent {
	var events []StreamEvent

	switch evt.Type {
	case "response.output_text.delta":
		if evt.Delta != "" {
			events = append(events, StreamEvent{
				Type:  StreamEventContentDelta,
				Delta: evt.Delta,
			})
		}

	case "response.completed":
		usage := fromResponsesUsage(evt.Response.Usage)
		events = append(events, StreamEvent{
			Type:  StreamEventDone,
			Usage: usage,
		})

	case "response.failed":
		events = append(events, StreamEvent{
			Type: StreamEventError,
			Err:  fmt.Errorf("responses API failed: %v", evt.Response.Error),
		})
	}

	return events
}

// fromResponsesResponse 将 Responses API 非流式响应转为规范格式。
func fromResponsesResponse(resp *responses.Response) *ChatResponse {
	if resp == nil {
		return nil
	}

	respMsg := &client.ResponseMessage{
		Role: "assistant",
	}

	var textParts []string
	var toolCalls []client.ToolCall

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				if content.Text != "" {
					textParts = append(textParts, content.Text)
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, client.ToolCall{
				Id:   item.ID,
				Type: "function",
				Function: client.FunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments.OfString,
				},
			})
		}
	}

	if len(textParts) > 0 {
		respMsg.Content = joinStrings(textParts, "\n")
	} else {
		respMsg.Content = nil
	}
	respMsg.ToolCalls = toolCalls

	return &ChatResponse{
		Message: respMsg,
		Usage:   fromResponsesUsage(resp.Usage),
		// Responses API doesn't have a direct finish_reason on the top level
		FinishReason: responsesStatusToFinishReason(resp.Status),
	}
}

func fromResponsesUsage(u responses.ResponseUsage) *client.Usage {
	if u.TotalTokens == 0 && u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return &client.Usage{
		PromptTokens:     int(u.InputTokens),
		CompletionTokens: int(u.OutputTokens),
		TotalTokens:      int(u.TotalTokens),
	}
}

func responsesStatusToFinishReason(status responses.ResponseStatus) string {
	switch status {
	case "completed":
		return "stop"
	case "incomplete":
		return "length"
	default:
		return "stop"
	}
}

// toResponsesRole 将角色字符串转为 Responses API 角色。
func toResponsesRole(role string) responses.EasyInputMessageRole {
	switch role {
	case "system":
		return responses.EasyInputMessageRoleSystem
	case "user":
		return responses.EasyInputMessageRoleUser
	case "assistant":
		return responses.EasyInputMessageRoleAssistant
	default:
		return responses.EasyInputMessageRoleUser
	}
}

// =============================================================================
// 工具函数
// =============================================================================

// msgContentString 提取消息的字符串内容。
func msgContentString(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case nil:
		return ""
	default:
		b, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprintf("%v", c)
		}
		return string(b)
	}
}

// joinStrings 用分隔符拼接字符串切片。
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

// toOpenAIChatTools 将规范格式工具转为 OpenAI Chat Completions 格式。
func toOpenAIChatTools(tools []client.Tool) []openai.ChatCompletionToolUnionParam {
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		result = append(result, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Function.Name,
			Description: oaiDesc(t.Function.Description),
			Parameters:  t.Function.Parameters.Properties,
		}))
	}
	return result
}
