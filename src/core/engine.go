package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// EventType represents the type of event yielded by the engine.
// EventType 表示引擎产生的事件类型。
type EventType int

const (
	EventText          EventType = iota // 流式文本片段
	EventToolCall                       // 工具调用请求
	EventToolExecuting                  // 工具正在执行
	EventToolResult                     // 工具执行结果
	EventWaiting                        // 文本输出完毕，等待工具调用
	EventError                          // 非致命错误
	EventUsage                          // Token 用量信息
)

// EngineEvent represents a single event from the engine during a turn.
// EngineEvent 表示引擎在一轮对话中产生的单个事件。
type EngineEvent struct {
	Type     EventType      // 事件类型
	Text     string         // EventText: 流式文本片段
	ToolName string         // EventToolCall/Executing/Result: 工具名称
	ToolID   string         // EventToolCall/Executing/Result: 工具调用 ID
	Input    map[string]any // EventToolCall/Executing/Result: 工具输入参数
	Result   string         // EventToolResult: 工具返回内容
	IsError  bool           // EventToolResult/EventError: 是否为错误
	Activity string         // EventToolCall: 人类可读的操作描述
}

// PermissionAction represents the result of a permission check.
// PermissionAction 表示权限检查的结果。
type PermissionAction int

const (
	PermissionAllow PermissionAction = iota // 允许执行
	PermissionDeny                          // 拒绝执行
	PermissionAsk                           // 需要用户确认
)

// PermissionChecker checks whether a tool call is allowed.
// PermissionChecker 检查工具调用是否被允许。
type PermissionChecker interface {
	Check(toolName string, input map[string]any) PermissionAction
}

// DefaultPermissionChecker allows all tool calls.
// DefaultPermissionChecker 允许所有工具调用。
type DefaultPermissionChecker struct{}

func (d *DefaultPermissionChecker) Check(toolName string, input map[string]any) PermissionAction {
	return PermissionAllow
}

// CostTracker tracks API usage and costs.
// CostTracker 跟踪 API 用量和费用。
type CostTracker interface {
	AddUsage(model string, usage map[string]int, apiDurationS float64) // 添加 token 用量
	AddLinesChanged(added, removed int)                                // 添加代码行变更
}

// SessionStore persists conversation messages.
// SessionStore 持久化对话消息。
type SessionStore interface {
	AppendMessage(message map[string]any) // 追加消息
	GetMessages() []map[string]any        // 获取所有消息
}

// EngineConfig holds configuration for the Engine.
// EngineConfig 保存引擎的配置。
type EngineConfig struct {
	Provider       string            // LLM 提供商 (openai / claude)
	Model          string            // 模型名称
	MaxTokens      int               // 最大生成 token 数
	APIKey         string            // API 密钥
	BaseURL        string            // API 基础 URL
	SystemPrompt   string            // 系统提示词
	Tools          []tool.BaseTool   // 可用工具列表
	Permission     PermissionChecker // 权限检查器
	CostTracker    CostTracker       // 费用追踪器
	SessionStore   SessionStore      // 会话存储
	RetryMax       int               // 最大重试次数
	RetryBaseDelay time.Duration     // 重试基础延迟
	RetryMaxDelay  time.Duration     // 重试最大延迟
}

// DefaultEngineConfig returns a config with sensible defaults.
// DefaultEngineConfig 返回带有合理默认值的配置。
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		Provider:       "openai",
		MaxTokens:      8192,
		SystemPrompt:   "You are a helpful assistant.",
		Permission:     &DefaultPermissionChecker{},
		RetryMax:       10,
		RetryBaseDelay: 500 * time.Millisecond,
		RetryMaxDelay:  32 * time.Second,
	}
}

// Engine is the core orchestrator that manages LLM calls, tool execution,
// and conversation state. It wraps Eino's ChatModelAgent with ReAct pattern.
// Engine 是核心编排器，管理 LLM 调用、工具执行和对话状态。
// 它封装了 Eino 的 ChatModelAgent，使用 ReAct 模式。
type Engine struct {
	config       *EngineConfig              // 配置
	chatModel    model.ToolCallingChatModel // 聊天模型
	agent        adk.Agent                  // Eino Agent
	runner       *adk.Runner                // Agent 运行器
	messages     []*schema.Message          // 对话消息历史
	mu           sync.RWMutex               // 并发锁
	aborted      bool                       // 是否已中止
	cancel       context.CancelFunc         // 当前轮次的取消函数
	turnStartLen int                        // 当前轮次开始时的消息长度
}

// NewEngine creates a new Engine instance with a pre-configured ToolCallingChatModel.
// NewEngine 使用预配置的 ToolCallingChatModel 创建新的 Engine 实例。
func NewEngine(ctx context.Context, cfg *EngineConfig, cm model.ToolCallingChatModel) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultEngineConfig()
	}
	if cm == nil {
		return nil, fmt.Errorf("chatModel cannot be nil") // chatModel 不能为空
	}

	e := &Engine{
		config:    cfg,
		chatModel: cm,
		messages:  make([]*schema.Message, 0),
	}

	if err := e.createAgent(ctx); err != nil {
		return nil, err
	}

	return e, nil
}

// createAgent creates or recreates the Eino ChatModelAgent and Runner.
// createAgent 创建或重新创建 Eino ChatModelAgent 和 Runner。
func (e *Engine) createAgent(ctx context.Context) error {
	agentConfig := &adk.ChatModelAgentConfig{
		Name:        "Engine",                                                // Agent 名称
		Description: "Core engine for LLM conversations with tool execution", // Agent 描述
		Instruction: e.config.SystemPrompt,                                   // 系统提示词
		Model:       e.chatModel,                                             // 聊天模型
		ToolsConfig: adk.ToolsConfig{ // 工具配置
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: e.config.Tools, // 工具列表
			},
		},
		MaxIterations: 20, // 最大 ReAct 迭代次数
	}

	agent, err := adk.NewChatModelAgent(ctx, agentConfig)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err) // 创建 agent 失败
	}

	e.agent = agent
	e.runner = adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent, // Agent 实例
		EnableStreaming: true,  // 启用流式输出
	})

	return nil
}

// GetMessages returns a copy of the current message history.
// GetMessages 返回当前消息历史的副本。
func (e *Engine) GetMessages() []*schema.Message {
	e.mu.RLock()
	defer e.mu.RUnlock()
	msgs := make([]*schema.Message, len(e.messages))
	copy(msgs, e.messages)
	return msgs
}

// SetMessages replaces the message history.
// SetMessages 替换消息历史。
func (e *Engine) SetMessages(messages []*schema.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.messages = messages
}

// SetTools replaces the tools used by the engine and recreates the agent.
// SetTools 替换引擎使用的工具并重新创建 agent。
func (e *Engine) SetTools(ctx context.Context, tools []tool.BaseTool) error {
	e.config.Tools = tools
	return e.createAgent(ctx)
}

// GetModel returns the current model name.
// GetModel 返回当前模型名称。
func (e *Engine) GetModel() string {
	return e.config.Model
}

// SetModel changes the model used by the engine and recreates the agent.
// SetModel 更改引擎使用的模型并重新创建 agent。
func (e *Engine) SetModel(ctx context.Context, cm model.ToolCallingChatModel, modelName string) error {
	e.chatModel = cm
	e.config.Model = modelName
	return e.createAgent(ctx)
}

// Abort cancels the current turn.
// Abort 立即取消当前轮次。
func (e *Engine) Abort() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.aborted = true
	if e.cancel != nil {
		e.cancel()
	}
}

// CancelTurn rolls back messages to the state before the current turn.
// CancelTurn 将消息回滚到当前轮次开始之前的状态。
func (e *Engine) CancelTurn() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.turnStartLen >= 0 && e.turnStartLen < len(e.messages) {
		e.messages = e.messages[:e.turnStartLen]
	}
	e.turnStartLen = -1
}

// Submit sends a user message and returns a channel of events.
// The channel is closed when the turn completes.
// Submit 发送用户消息并返回事件通道。
// 当轮次完成时通道被关闭。
func (e *Engine) Submit(ctx context.Context, userInput string) <-chan EngineEvent {
	events := make(chan EngineEvent, 64) // 事件缓冲通道

	go func() {
		defer close(events)

		// 记录轮次开始时的消息长度，并追加用户消息
		e.mu.Lock()
		e.aborted = false
		e.turnStartLen = len(e.messages)
		e.messages = append(e.messages, schema.UserMessage(userInput))
		e.mu.Unlock()

		// 为当前轮次创建可取消的 context
		ctx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.cancel = cancel
		e.mu.Unlock()

		defer func() {
			e.mu.Lock()
			e.cancel = nil
			e.mu.Unlock()
			cancel()
		}()

		// 使用流式模式运行 agent
		iter := e.runner.Run(ctx, e.messages)

		for {
			// 检查是否已中止
			if e.isAborted() {
				e.CancelTurn()
				events <- EngineEvent{Type: EventError, Text: "Turn aborted", IsError: true} // 轮次已中止
				return
			}

			event, ok := iter.Next()
			if !ok {
				break // 没有更多事件
			}

			if event.Err != nil {
				events <- EngineEvent{Type: EventError, Text: event.Err.Error(), IsError: true}
				return
			}

			// 处理输出事件
			if event.Output != nil && event.Output.MessageOutput != nil {
				mv := event.Output.MessageOutput

				if mv.IsStreaming {
					// 流式读取文本片段
					for {
						msg, err := mv.MessageStream.Recv()
						if err == io.EOF {
							break // 流结束
						}
						if err != nil {
							events <- EngineEvent{Type: EventError, Text: err.Error(), IsError: true}
							return
						}
						// 检查是否为工具相关消息
						if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
							// 助手消息中的工具调用请求
							for _, tc := range msg.ToolCalls {
								events <- EngineEvent{
									Type:     EventToolCall,
									ToolName: tc.Function.Name,
									ToolID:   tc.ID,
									Input:    parseToolArgs(tc.Function.Arguments),
								}
							}
						} else if msg.Role == schema.Tool {
							// 工具执行结果
							events <- EngineEvent{
								Type:     EventToolResult,
								ToolName: msg.ToolName,
								ToolID:   msg.ToolCallID,
								Result:   msg.Content,
							}
						} else if msg.Content != "" {
							// 普通文本输出
							events <- EngineEvent{Type: EventText, Text: msg.Content}
						}
					}
					events <- EngineEvent{Type: EventWaiting} // 文本输出完毕
				} else {
					// 非流式消息
					if mv.Message != nil {
						msg := mv.Message
						if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
							// 助手消息中的工具调用请求
							for _, tc := range msg.ToolCalls {
								events <- EngineEvent{
									Type:     EventToolCall,
									ToolName: tc.Function.Name,
									ToolID:   tc.ID,
									Input:    parseToolArgs(tc.Function.Arguments),
								}
							}
						} else if msg.Role == schema.Tool {
							// 工具执行结果
							events <- EngineEvent{
								Type:     EventToolResult,
								ToolName: msg.ToolName,
								ToolID:   msg.ToolCallID,
								Result:   msg.Content,
							}
						} else if msg.Content != "" {
							// 普通文本输出
							events <- EngineEvent{Type: EventText, Text: msg.Content}
						}
					}
				}
			}

			// 处理工具调用中断（human-in-the-loop）
			if event.Action != nil {
				if event.Action.Interrupted != nil {
					events <- EngineEvent{
						Type:     EventToolCall,
						ToolName: "interrupt",
						Text:     fmt.Sprintf("Interrupted: %v", event.Action.Interrupted),
					}
				}
			}
		}

		// 持久化最终消息
		e.mu.Lock()
		e.persistMessages()
		e.mu.Unlock()
	}()

	return events
}

// SubmitSync is a synchronous version of Submit that collects all events.
// SubmitSync 是 Submit 的同步版本，收集所有事件后返回。
func (e *Engine) SubmitSync(ctx context.Context, userInput string) ([]EngineEvent, error) {
	events := e.Submit(ctx, userInput)
	var result []EngineEvent
	for ev := range events {
		result = append(result, ev)
		if ev.Type == EventError && ev.IsError {
			return result, fmt.Errorf("%s", ev.Text)
		}
	}
	return result, nil
}

// isAborted checks if the engine has been aborted.
// isAborted 检查引擎是否已被中止。
func (e *Engine) isAborted() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.aborted
}

// persistMessages saves messages to session store if available.
// persistMessages 将消息保存到会话存储（如果可用）。
func (e *Engine) persistMessages() {
	if e.config.SessionStore == nil {
		return // 没有配置会话存储
	}
	for _, msg := range e.messages {
		m := map[string]any{
			"role":    string(msg.Role), // 角色
			"content": msg.Content,      // 内容
		}
		e.config.SessionStore.AppendMessage(m)
	}
}

// RetryDelay computes exponential backoff with jitter.
// RetryDelay 计算带抖动的指数退避延迟。
func RetryDelay(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	delay := baseDelay * time.Duration(1<<uint(attempt)) // 指数增长
	if delay > maxDelay {
		delay = maxDelay // 不超过最大延迟
	}
	// 添加 0-25% 的随机抖动
	jitter := time.Duration(float64(delay) * 0.25 * float64(time.Now().UnixNano()%100) / 100.0)
	return delay + jitter
}

// WrapWithRetry wraps a function with retry logic.
// WrapWithRetry 为函数添加重试逻辑。
func WrapWithRetry(ctx context.Context, maxRetries int, baseDelay, maxDelay time.Duration, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if attempt < maxRetries-1 {
				delay := RetryDelay(attempt, baseDelay, maxDelay)
				select {
				case <-ctx.Done():
					return ctx.Err() // context 已取消
				case <-time.After(delay):
					continue // 等待后重试
				}
			}
		} else {
			return nil // 成功
		}
	}
	return fmt.Errorf("after %d retries: %w", maxRetries, lastErr) // 重试耗尽
}

// parseToolArgs parses JSON arguments string into a map.
// parseToolArgs 将 JSON 参数字符串解析为 map。
func parseToolArgs(args string) map[string]any {
	if args == "" {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return map[string]any{"raw": args} // 解析失败时返回原始字符串
	}
	return result
}
