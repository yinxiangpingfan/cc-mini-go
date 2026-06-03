// Package main（example/hooks）演示 s08 hook 机制的用法：
// 如何写 handler、如何注册、三种退出码（观察/拦截/注入）分别长什么样，
// 以及怎么用 WithHooks 把它们接进真正的 agent。
//
// 直接运行：在本目录执行 `go run .`（纯离线演示，不需要 API key 或配置）。
package main

import (
	"fmt"
	"strings"

	"github.com/yinxiangpingfan/cc-mini-go/agent/core"
)

// 下面是四个示例 handler，覆盖 hook 的三种作用：观察 / 拦截 / 注入。
// handler 统一签名 func(payload map[string]any) core.HookResult；
// 上下文都从 payload 里取（不同事件塞的字段不同，见各函数）。

// Welcome 是一个 SessionStart handler——观察型（exit 0），只打一行欢迎语，不干预流程。
func Welcome(_ map[string]any) core.HookResult {
	fmt.Println("👋 欢迎使用 cc-mini-go（示例 SessionStart hook 已生效）")
	return core.HookResult{ExitCode: core.HookContinue}
}

// BashGuard 是一个 PreToolUse handler——拦截型（exit 1）。
// 只盯 bash，命中危险子串就拦下，工具不会被执行。payload 里有 tool_name 和 input。
func BashGuard(p map[string]any) core.HookResult {
	if str(p, "tool_name") != "bash" {
		return core.HookResult{ExitCode: core.HookContinue} // 不是 bash，放过
	}
	cmd := inputStr(p, "command")
	if strings.Contains(cmd, "rm -rf") {
		return core.HookResult{ExitCode: core.HookBlock, Message: "示例护栏：禁止 rm -rf"}
	}
	return core.HookResult{ExitCode: core.HookContinue}
}

// WriteNotice 是一个 PreToolUse handler——注入型（exit 2）。
// 改动 go.mod 时不拦，但替我们给模型捎一句提醒；工具照常执行。
func WriteNotice(p map[string]any) core.HookResult {
	name := str(p, "tool_name")
	if name != "write_file" && name != "edit_file" {
		return core.HookResult{ExitCode: core.HookContinue}
	}
	if strings.HasSuffix(inputStr(p, "file_path"), "go.mod") {
		return core.HookResult{ExitCode: core.HookInject, Message: "提示：go.mod 是依赖清单，修改后记得跑 go mod tidy。"}
	}
	return core.HookResult{ExitCode: core.HookContinue}
}

// Audit 是一个 PostToolUse handler——观察型（exit 0）。
// 工具跑完后记一条审计；这时 payload 多了 output（工具结果）。
func Audit(p map[string]any) core.HookResult {
	fmt.Printf("📝 [audit] 工具 %s 执行完毕，结果预览：%s\n",
		str(p, "tool_name"), preview(str(p, "output"), 40))
	return core.HookResult{ExitCode: core.HookContinue}
}

// RegisterAll 把以上 handler 一次性注册到 runner——这就是「布线」。
// 同一事件可注册多个，按注册顺序触发（这里 PreToolUse 挂了 BashGuard、WriteNotice 两个）。
func RegisterAll(r *core.HookRunner) {
	r.Register(core.HookSessionStart, Welcome)
	r.Register(core.HookPreToolUse, BashGuard)
	r.Register(core.HookPreToolUse, WriteNotice)
	r.Register(core.HookPostToolUse, Audit)
}

// ——以下是从 payload 安全取值的小工具（始终用 comma-ok，避免类型断言 panic）——

// str 取 payload 顶层的字符串字段。
func str(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return s
}

// inputStr 取 payload["input"]（工具参数）里的字符串字段，如 command / file_path。
func inputStr(p map[string]any, key string) string {
	in, _ := p["input"].(map[string]any)
	s, _ := in[key].(string)
	return s
}

// preview 按字符（rune）截断，避免切坏多字节中文。
func preview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
