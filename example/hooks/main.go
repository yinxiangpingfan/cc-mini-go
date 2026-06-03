package main

import (
	"fmt"
	"log/slog"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
)

// main 离线演示：自己构造 HookRunner，手动 Run 各个事件，把三种退出码的效果打出来。
// 这正是 agent 主循环在工具前后做的事，只是这里用假 payload 直接触发，不需要真实 LLM。
func main() {
	// 1. 建表 + 布线（启动时做一次）
	r := core.NewHookRunner()
	RegisterAll(r)

	// 2. SessionStart：观察型，打欢迎语
	fmt.Println("=== 1. SessionStart（观察 exit 0：打欢迎语）===")
	r.Run(core.HookSessionStart, nil)

	// 3. PreToolUse 危险命令：拦截型
	fmt.Println("\n=== 2. PreToolUse 危险命令（拦截 exit 1）===")
	res := r.Run(core.HookPreToolUse, map[string]any{
		"tool_name": "bash",
		"input":     map[string]any{"command": "rm -rf /tmp/x"},
	})
	fmt.Printf("→ exit=%d message=%q\n", res.ExitCode, res.Message)
	fmt.Printf("→ 主循环据此跳过执行，回传：%s\n", core.HookBlockedResult(res.Message))

	// 4. PreToolUse 改 go.mod：注入型
	fmt.Println("\n=== 3. PreToolUse 改 go.mod（注入 exit 2）===")
	res = r.Run(core.HookPreToolUse, map[string]any{
		"tool_name": "write_file",
		"input":     map[string]any{"file_path": "go.mod"},
	})
	fmt.Printf("→ exit=%d message=%q\n", res.ExitCode, res.Message)
	fmt.Println("→ 工具照常执行，这句话会作为补充消息注入给模型")

	// 5. 普通 bash：无人拦截 → 继续
	fmt.Println("\n=== 4. PreToolUse 普通命令（无 handler 命中 → 继续 exit 0）===")
	res = r.Run(core.HookPreToolUse, map[string]any{
		"tool_name": "bash",
		"input":     map[string]any{"command": "go build ./..."},
	})
	fmt.Printf("→ exit=%d → 放行，正常执行\n", res.ExitCode)

	// 6. PostToolUse：观察型，记审计（payload 多带 output）
	fmt.Println("\n=== 5. PostToolUse（观察 exit 0：记审计）===")
	r.Run(core.HookPostToolUse, map[string]any{
		"tool_name": "read_file",
		"input":     map[string]any{"file_path": "main.go"},
		"output":    "package main\n\nimport \"fmt\"\n...",
	})

	fmt.Println("\n=== 接进真实 agent 的写法见本文件 buildAgentWithHooks() ===")
}

// buildAgentWithHooks 演示在真实项目里怎么把 hook 接进 agent：
// 建 runner → RegisterAll → 用 agent.WithHooks(r) 作为构造选项注入即可，主循环代码一行不改。
// 这里只构造不运行（运行需要 ~/.cc_mini_go/setting.json 与可用的 API），故 main 不调用它。
func buildAgentWithHooks() (*agent.ChatCompletionAgent, error) {
	cf, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		return nil, err
	}
	call := client.NewCall(cl, client.NewChatCompletionMessage(), slog.Default())

	// 关键三步：建表、布线、注入。
	r := core.NewHookRunner()
	RegisterAll(r)
	return agent.NewChatCompletionAgent(&cf, call, agent.WithHooks(r)), nil
}
