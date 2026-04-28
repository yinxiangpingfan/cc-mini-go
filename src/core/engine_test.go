package core

import (
	"cc_mini/src/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestSomething(t *testing.T) {
	config, _ := config.NewConfig("../config/config.json")
	ec := EngineConfig{
		Provider:     "openai",
		Model:        config.Model,
		APIKey:       config.APIKey,
		BaseURL:      config.BaseUrl,
		SystemPrompt: "你是一个有用的代码助手。可以执行工具修改文件内容，也可以回答问题",
		Tools:        []tool.BaseTool{},
	}
	om, _ := NewOpenAIChatModel(context.Background(), config.APIKey, ec.Model, config.BaseUrl, "high")
	if om == nil {
		t.Error("Failed to create OpenAIChatModel")
	}
	e, _ := NewEngine(context.Background(), &ec, om.model)
	ctx := context.Background()
	err := e.createAgent(ctx)
	if err != nil {
		t.Errorf("Failed to create agent: %v", err)
	}

	ch := e.Submit(ctx, "你好啊")
	for {
		ev, ok := <-ch
		if !ok {
			break // channel 已关闭
		}
		fmt.Println(ev)
	}
}

// --- parseToolArgs ---

func TestParseToolArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]any
		wantNil bool
	}{
		{
			name:    "空字符串返回 nil",
			input:   "",
			wantNil: true,
		},
		{
			name:  "正常 JSON",
			input: `{"file_path":"/tmp/test.go","offset":10}`,
			want:  map[string]any{"file_path": "/tmp/test.go", "offset": float64(10)},
		},
		{
			name:  "无效 JSON 返回 raw",
			input: "not json",
			want:  map[string]any{"raw": "not json"},
		},
		{
			name:  "嵌套 JSON",
			input: `{"a":{"b":"c"},"d":42}`,
			want: map[string]any{
				"a": map[string]any{"b": "c"},
				"d": float64(42),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseToolArgs(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("期望 nil, 得到 %v", got)
				}
				return
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tt.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("期望 %s, 得到 %s", wantJSON, gotJSON)
			}
		})
	}
}

// --- RetryDelay ---

func TestRetryDelay(t *testing.T) {
	base := 100 * time.Millisecond
	max := 10 * time.Second

	// 每次重试延迟应 >= base * 2^attempt
	for attempt := 0; attempt < 4; attempt++ {
		delay := RetryDelay(attempt, base, max)
		minExpected := base * time.Duration(1<<uint(attempt))
		if delay < minExpected {
			t.Errorf("attempt %d: 期望延迟 >= %v, 得到 %v", attempt, minExpected, delay)
		}
		if delay > max {
			t.Errorf("attempt %d: 期望延迟 <= %v, 得到 %v", attempt, max, delay)
		}
	}

	// 高 attempt 应被 max 限制
	delay := RetryDelay(100, base, max)
	if delay > max+max/4 { // 允许 jitter 上限
		t.Errorf("attempt 100: 期望延迟 <= %v, 得到 %v", max+max/4, delay)
	}
}

// --- WrapWithRetry ---

func TestWrapWithRetry_成功不重试(t *testing.T) {
	calls := 0
	err := WrapWithRetry(context.Background(), 3, 10*time.Millisecond, 1*time.Second, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("期望无错误, 得到 %v", err)
	}
	if calls != 1 {
		t.Errorf("期望调用 1 次, 实际 %d 次", calls)
	}
}

func TestWrapWithRetry_重试后成功(t *testing.T) {
	calls := 0
	err := WrapWithRetry(context.Background(), 3, 10*time.Millisecond, 1*time.Second, func() error {
		calls++
		if calls < 3 {
			return errors.New("暂时失败")
		}
		return nil
	})
	if err != nil {
		t.Errorf("期望无错误, 得到 %v", err)
	}
	if calls != 3 {
		t.Errorf("期望调用 3 次, 实际 %d 次", calls)
	}
}

func TestWrapWithRetry_全部失败(t *testing.T) {
	calls := 0
	err := WrapWithRetry(context.Background(), 3, 1*time.Millisecond, 10*time.Millisecond, func() error {
		calls++
		return errors.New("持续失败")
	})
	if err == nil {
		t.Error("期望有错误, 得到 nil")
	}
	if calls != 3 {
		t.Errorf("期望调用 3 次, 实际 %d 次", calls)
	}
}

func TestWrapWithRetry_Context取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	calls := 0
	err := WrapWithRetry(ctx, 5, 10*time.Millisecond, 1*time.Second, func() error {
		calls++
		return errors.New("失败")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("期望 context.Canceled, 得到 %v", err)
	}
}

// --- DefaultPermissionChecker ---

func TestDefaultPermissionChecker(t *testing.T) {
	checker := &DefaultPermissionChecker{}
	if checker.Check("任意工具", map[string]any{}) != PermissionAllow {
		t.Error("DefaultPermissionChecker 应允许所有调用")
	}
}

// --- EngineConfig ---

func TestDefaultEngineConfig(t *testing.T) {
	cfg := DefaultEngineConfig()
	if cfg.Provider != "openai" {
		t.Errorf("期望 provider 'openai', 得到 '%s'", cfg.Provider)
	}
	if cfg.MaxTokens != 8192 {
		t.Errorf("期望 MaxTokens 8192, 得到 %d", cfg.MaxTokens)
	}
	if cfg.RetryMax != 10 {
		t.Errorf("期望 RetryMax 10, 得到 %d", cfg.RetryMax)
	}
	if cfg.RetryBaseDelay != 500*time.Millisecond {
		t.Errorf("期望 RetryBaseDelay 500ms, 得到 %v", cfg.RetryBaseDelay)
	}
	if cfg.RetryMaxDelay != 32*time.Second {
		t.Errorf("期望 RetryMaxDelay 32s, 得到 %v", cfg.RetryMaxDelay)
	}
	if cfg.Permission == nil {
		t.Error("期望 Permission 非 nil")
	}
	if cfg.SystemPrompt != "You are a helpful assistant." {
		t.Errorf("期望默认 SystemPrompt, 得到 '%s'", cfg.SystemPrompt)
	}
}

// --- EngineEvent ---

func TestEngineEventTypes_不重复(t *testing.T) {
	types := []EventType{EventText, EventToolCall, EventToolExecuting, EventToolResult, EventWaiting, EventError, EventUsage}
	seen := make(map[EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("重复的 EventType: %v", et)
		}
		seen[et] = true
	}
}

func TestPermissionActions_不重复(t *testing.T) {
	actions := []PermissionAction{PermissionAllow, PermissionDeny, PermissionAsk}
	seen := make(map[PermissionAction]bool)
	for _, a := range actions {
		if seen[a] {
			t.Errorf("重复的 PermissionAction: %v", a)
		}
		seen[a] = true
	}
}

// --- Engine 消息操作 ---

func TestEngine_GetSetMessages(t *testing.T) {
	e := &Engine{
		messages: []*schema.Message{
			schema.UserMessage("hello"),
		},
	}

	// GetMessages 返回浅拷贝（指针切片副本，Message 本身共享）
	msgs := e.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("期望 1 条消息, 得到 %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("期望内容 'hello', 得到 '%s'", msgs[0].Content)
	}

	// 浅拷贝：修改指针指向的内容会影响原始消息（共享 Message 结构体）
	msgs[0].Content = "modified"
	if e.messages[0].Content != "modified" {
		t.Error("浅拷贝：修改指针内容应影响原始消息")
	}

	// 但替换/追加切片元素不影响原始切片
	msgs = append(msgs, schema.AssistantMessage("reply", nil))
	if len(e.GetMessages()) != 1 {
		t.Error("向副本追加元素不应影响原始切片")
	}

	// SetMessages 替换消息
	e.SetMessages([]*schema.Message{
		schema.UserMessage("new"),
		schema.AssistantMessage("reply", nil),
	})
	if len(e.GetMessages()) != 2 {
		t.Errorf("期望 2 条消息, 得到 %d", len(e.GetMessages()))
	}
}

// --- Engine Abort / CancelTurn ---

func TestEngine_Abort(t *testing.T) {
	e := &Engine{}

	// 设置 cancel 函数
	cancelled := false
	e.cancel = func() { cancelled = true }

	e.Abort()

	if !e.aborted {
		t.Error("期望 aborted=true")
	}
	if !cancelled {
		t.Error("期望 cancel 被调用")
	}
}

func TestEngine_CancelTurn(t *testing.T) {
	e := &Engine{
		messages: []*schema.Message{
			schema.UserMessage("msg1"),
			schema.AssistantMessage("reply1", nil),
			schema.UserMessage("msg2"),
		},
		turnStartLen: 2, // 第二轮开始前有 2 条消息
	}

	e.CancelTurn()

	if len(e.GetMessages()) != 2 {
		t.Errorf("期望回滚到 2 条消息, 得到 %d", len(e.GetMessages()))
	}
	if e.turnStartLen != -1 {
		t.Errorf("期望 turnStartLen=-1, 得到 %d", e.turnStartLen)
	}
}

func TestEngine_CancelTurn_无效范围(t *testing.T) {
	e := &Engine{
		messages:     []*schema.Message{schema.UserMessage("msg")},
		turnStartLen: 10, // 超出范围
	}

	e.CancelTurn()

	// 不应 panic，消息保持不变
	if len(e.GetMessages()) != 1 {
		t.Errorf("期望消息不变, 得到 %d", len(e.GetMessages()))
	}
}

// --- Engine NewEngine 错误处理 ---

func TestNewEngine_ConfigNil(t *testing.T) {
	// 提供 mock ToolCallingChatModel，cfg 为 nil 应使用默认配置
	// 这里只测试 cfg=nil 的情况，cm 也需要非 nil
	// 由于需要真实的 ToolCallingChatModel，跳过实际创建
	// 只测试 parseToolArgs 等纯函数
}

// --- Engine isAborted ---

func TestEngine_isAborted(t *testing.T) {
	e := &Engine{}
	if e.isAborted() {
		t.Error("初始状态应为 false")
	}
	e.aborted = true
	if !e.isAborted() {
		t.Error("设置后应为 true")
	}
}

// --- Engine GetModel ---

func TestEngine_GetModel(t *testing.T) {
	e := &Engine{
		config: &EngineConfig{Model: "gpt-4o"},
	}
	if e.GetModel() != "gpt-4o" {
		t.Errorf("期望 'gpt-4o', 得到 '%s'", e.GetModel())
	}
}

// --- EngineEvent 结构体 ---

func TestEngineEvent_ToolCall(t *testing.T) {
	ev := EngineEvent{
		Type:     EventToolCall,
		ToolName: "Read",
		ToolID:   "call_123",
		Input:    map[string]any{"file_path": "/tmp/test.go"},
	}

	if ev.Type != EventToolCall {
		t.Errorf("期望 EventToolCall, 得到 %v", ev.Type)
	}
	if ev.ToolName != "Read" {
		t.Errorf("期望 ToolName 'Read', 得到 '%s'", ev.ToolName)
	}
	if ev.Input["file_path"] != "/tmp/test.go" {
		t.Errorf("期望 file_path '/tmp/test.go', 得到 '%v'", ev.Input["file_path"])
	}
}

func TestEngineEvent_ToolResult(t *testing.T) {
	ev := EngineEvent{
		Type:     EventToolResult,
		ToolName: "Bash",
		ToolID:   "call_456",
		Result:   "file1.go\nfile2.go",
		IsError:  false,
	}

	if ev.Type != EventToolResult {
		t.Errorf("期望 EventToolResult, 得到 %v", ev.Type)
	}
	if ev.IsError {
		t.Error("期望 IsError=false")
	}
}

func TestEngineEvent_Error(t *testing.T) {
	ev := EngineEvent{
		Type:    EventError,
		Text:    "API rate limit exceeded",
		IsError: true,
	}

	if ev.Type != EventError {
		t.Errorf("期望 EventError, 得到 %v", ev.Type)
	}
	if !ev.IsError {
		t.Error("期望 IsError=true")
	}
}
