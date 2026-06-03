package agent

import (
	"context"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools"
)

// Hook 系统接线层（s08 第 2 步）：把纯插件机制 core.HookRunner 接进工具循环。
// 工具执行前后各暴露一个时机——PreToolUse 可拦截（exit 1）或注入消息（exit 2）；
// PostToolUse 可在工具跑完后补一条说明（exit 2）。注入的消息统一在本轮所有工具结果
// 之后再 append：保证 assistant 的每个 tool_call 紧跟其 result、不被打断（协议要求一一配对）。

// WithHooks 注入 hook 运行器。未注入（nil）时所有时机都直接放行，保持原有行为。
func WithHooks(r *core.HookRunner) AgentOption {
	return func(a *ChatCompletionAgent) { a.hooks = r }
}

// runHook 触发某事件；未配置运行器（nil）时等价于「继续」。
func (a *ChatCompletionAgent) runHook(event string, payload map[string]any) core.HookResult {
	if a.hooks == nil {
		return core.HookResult{ExitCode: core.HookContinue}
	}
	return a.hooks.Run(event, payload)
}

// execToolWithHooks 执行一次工具调用的完整前置/后置流程：
// PreToolUse → 权限闸 → 执行工具 → PostToolUse。
// 返回工具结果 content、可能的图片（imageURI/isImage），以及需要在本轮所有工具结果之后
// 再注入的补充消息 injected（来自 exit 2）。被拦截时不执行工具，但仍返回一条配对的 content。
func (a *ChatCompletionAgent) execToolWithHooks(
	ctx context.Context, toolID, name, rawArgs string, args map[string]any, f agent_tools.ToolFunc,
) (content, imageURI string, isImage bool, injected []string) {
	// 1. PreToolUse：执行前的扩展点。拦截即返回配对的 tool 结果、跳过执行。
	pre := a.runHook(core.HookPreToolUse, map[string]any{"tool_name": name, "input": args})
	if pre.ExitCode == core.HookBlock {
		return core.HookBlockedResult(pre.Message), "", false, nil
	}
	if pre.ExitCode == core.HookInject && pre.Message != "" {
		injected = append(injected, pre.Message)
	}

	// 2. 权限闸：被拦同样返回配对结果，但 PreToolUse 已收集的注入消息仍要带出去。
	if denied, blocked := a.gateToolCall(ctx, toolID, name, rawArgs, args); blocked {
		return denied, "", false, injected
	}

	// 3. 执行工具：大结果落盘只留预览；图片结果拆成「文字摘要 + data URI」。
	res := agent_tools.PersistLargeOutput(name, toolID, safeToolCall(ctx, name, f, args))
	content, imageURI, isImage = agent_tools.SplitImageResult(res)

	// 4. PostToolUse：执行后的扩展点，可追加一条补充说明给模型。
	post := a.runHook(core.HookPostToolUse, map[string]any{"tool_name": name, "input": args, "output": content})
	if post.ExitCode == core.HookInject && post.Message != "" {
		injected = append(injected, post.Message)
	}

	return content, imageURI, isImage, injected
}
