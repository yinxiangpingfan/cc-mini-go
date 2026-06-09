package test

import (
	"context"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent"
	tools "github.com/yinxiangpingfan/cc-mini-go/agent_tools/ctxmgmt"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/log"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// TestAutoCompact_TriggersWithTinyWindow 端到端验证 token 阈值真的会触发压缩：
// 把模型窗口设小，使阈值 = 34000−20000−13000 = 1000 token，再喂一段超过它的历史，
// 跑完后历史应被 CompactHistory 折叠成单条「压缩提示 + 摘要」消息。真打 API。
func TestAutoCompact_TriggersWithTinyWindow(t *testing.T) {
	cf, err := config.GetConfig()
	if err != nil {
		t.Fatal(err, "Failed to get config")
	}
	cf.MaxContextTokens = 34000 // AutoCompactThreshold => 1000 token，极易触发
	if got := tools.AutoCompactThreshold(cf.ContextWindow()); got != 1000 {
		t.Fatalf("precondition: threshold = %d, want 1000", got)
	}

	cl, err := client.Init(cf.ApiUrl, cf.ApiKey)
	if err != nil {
		t.Fatal(err)
	}
	call := client.NewCall(cl, client.NewChatCompletionMessage(), log.InitLogger())
	ag := agent.NewChatCompletionAgent(&cf, call)

	// 撑大历史：约 1 万字 → 估算远超 1000 token 阈值
	filler := strings.Repeat("这是一段用于撑大上下文的填充文字。", 600)
	history := []any{
		client.Message{Role: "user", Content: filler + "\n\n上面是背景。现在只需回复两个字：收到"},
	}

	out, err := ag.Agent(context.Background(), history, prompt.SystemPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("empty result")
	}

	// 压缩后，历史被替换成单条以 CompactNotice 开头的 user 消息（其后才是本轮新回复）
	first, ok := out[0].(client.Message)
	if !ok {
		t.Fatalf("first message type = %T, want client.Message", out[0])
	}
	content, _ := first.Content.(string)
	if !strings.Contains(content, prompt.CompactNotice) {
		t.Fatalf("历史未被压缩：首条消息不含 CompactNotice。\n首条内容前 120 字：%.120s", content)
	}
	t.Logf("✓ 压缩已触发；压缩后历史长度=%d，首条为摘要消息", len(out))
}
