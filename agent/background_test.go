package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/system"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func TestInjectBackgroundNotifications(t *testing.T) {
	old := shared.SessionStorageDir
	shared.SessionStorageDir = t.TempDir()
	t.Cleanup(func() { shared.SessionStorageDir = old })

	a := &ChatCompletionAgent{
		call:       &client.Call{Cm: client.NewChatCompletionMessage()},
		background: system.NewBackgroundManager(),
	}
	task, err := a.background.Run("printf 'tests passed'", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		msgs := a.injectBackgroundNotifications(nil)
		if len(msgs) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		msg, ok := msgs[0].(client.Message)
		if !ok {
			t.Fatalf("message type = %T", msgs[0])
		}
		content, ok := msg.Content.(string)
		if !ok {
			t.Fatalf("content type = %T", msg.Content)
		}
		if msg.Role != "user" || !strings.Contains(content, "<background-results>") || !strings.Contains(content, "tests passed") {
			t.Fatalf("unexpected injected message: %#v", msg)
		}
		if len(a.background.DrainNotifications()) != 0 {
			t.Fatal("notifications should drain after injection")
		}
		return
	}
	t.Fatalf("background task %s did not produce injected notification", task.ID)
}

func TestInjectRuntimeNotificationsIncludesCron(t *testing.T) {
	old := shared.SessionStorageDir
	shared.SessionStorageDir = t.TempDir()
	t.Cleanup(func() { shared.SessionStorageDir = old })

	a := &ChatCompletionAgent{
		call:       &client.Call{Cm: client.NewChatCompletionMessage()},
		background: system.NewBackgroundManager(),
		cron:       system.NewCronScheduler(),
	}
	a.cron.Create("* * * * *", "scheduled follow-up", false, false)
	a.cron.CheckNow(time.Now())

	msgs := a.injectRuntimeNotifications(nil)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	msg, ok := msgs[0].(client.Message)
	if !ok {
		t.Fatalf("message type = %T", msgs[0])
	}
	content, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("content type = %T", msg.Content)
	}
	if msg.Role != "user" || !strings.Contains(content, "<scheduled-prompts>") || !strings.Contains(content, "scheduled follow-up") {
		t.Fatalf("unexpected injected message: %#v", msg)
	}
	if len(a.cron.DrainNotifications()) != 0 {
		t.Fatal("cron notifications should drain after injection")
	}
}
