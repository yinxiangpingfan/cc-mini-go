package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func newTestTeamManager(t *testing.T) *TeammateManager {
	t.Helper()
	oldProject := shared.ProjectStorageDir
	oldSession := shared.SessionStorageDir
	shared.ProjectStorageDir = t.TempDir()
	shared.SessionStorageDir = t.TempDir()
	t.Cleanup(func() {
		shared.ProjectStorageDir = oldProject
		shared.SessionStorageDir = oldSession
	})
	m := NewTeammateManager(nil, "test-model")
	m.Start()
	return m
}

func waitForTeamStatus(t *testing.T, mgr *TeammateManager, name string, status TeamStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		mgr.loadConfigLocked()
		idx := mgr.memberIndexLocked(name)
		got := TeamStatus("")
		if idx >= 0 {
			got = mgr.config.Members[idx].Status
		}
		mgr.mu.Unlock()
		if got == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("teammate %s did not reach status %s", name, status)
}

func fastTeamRetries(t *testing.T) {
	t.Helper()
	oldBase, oldMaxDelay, oldMaxRetries := teamBaseRetryDelay, teamMaxRetryDelay, teamMaxLLMRetries
	teamBaseRetryDelay = time.Millisecond
	teamMaxRetryDelay = 2 * time.Millisecond
	teamMaxLLMRetries = 3
	t.Cleanup(func() {
		teamBaseRetryDelay = oldBase
		teamMaxRetryDelay = oldMaxDelay
		teamMaxLLMRetries = oldMaxRetries
	})
}

func TestMessageBusSendReadAndValidateType(t *testing.T) {
	bus := NewMessageBus(t.TempDir())
	if _, err := bus.Send("lead", "alice", "hello", "message"); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Send("lead", "alice", "bad", "invalid"); err == nil {
		t.Fatal("expected invalid message type error")
	}
	messages, err := bus.ReadInbox("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].From != "lead" || messages[0].Content != "hello" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	messages, err = bus.ReadInbox("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("inbox should be drained, got %#v", messages)
	}
}

func TestTeammateSendWakesIdleMember(t *testing.T) {
	mgr := newTestTeamManager(t)
	runCount := 0
	messageCounts := []int{}
	mgr.runTurn = func(ctx context.Context, name, role string, messages []any) ([]any, bool, string, error) {
		runCount++
		messageCounts = append(messageCounts, len(messages))
		return append(messages, *client.NewChatCompletionMessage().NewAssistantMessage("done")), false, "done", nil
	}

	if _, err := mgr.Spawn("alice", "coder", "initial task"); err != nil {
		t.Fatal(err)
	}
	waitForTeamStatus(t, mgr, "alice", TeamIdle)
	if runCount != 1 {
		t.Fatalf("runCount after spawn = %d, want 1", runCount)
	}

	if _, err := mgr.Send("lead", "alice", "follow up", "message"); err != nil {
		t.Fatal(err)
	}
	waitForTeamStatus(t, mgr, "alice", TeamIdle)
	if runCount != 2 {
		t.Fatalf("idle teammate was not woken, runCount = %d", runCount)
	}
	if len(messageCounts) != 2 || messageCounts[0] != 1 || messageCounts[1] != 1 {
		t.Fatalf("idle wake should start from fresh context plus inbox, message counts = %#v", messageCounts)
	}
}

func TestTeammateSendLazilyCreatesWorkerFromRoster(t *testing.T) {
	mgr := newTestTeamManager(t)
	runCount := 0
	mgr.runTurn = func(ctx context.Context, name, role string, messages []any) ([]any, bool, string, error) {
		runCount++
		return messages, false, "ok", nil
	}
	if _, err := mgr.Spawn("alice", "coder", "initial task"); err != nil {
		t.Fatal(err)
	}
	waitForTeamStatus(t, mgr, "alice", TeamIdle)

	mgr.mu.Lock()
	if _, exists := mgr.workers["alice"]; exists {
		t.Fatal("worker should be discarded after going idle")
	}
	mgr.mu.Unlock()

	if _, err := mgr.Send("lead", "alice", "wake from roster", "message"); err != nil {
		t.Fatal(err)
	}
	waitForTeamStatus(t, mgr, "alice", TeamIdle)
	if runCount != 2 {
		t.Fatalf("expected roster-backed wake to run teammate again, runCount = %d", runCount)
	}
}

func TestTeammateSendUnknownDoesNotCreateInbox(t *testing.T) {
	mgr := newTestTeamManager(t)
	if _, err := mgr.Send("lead", "missing", "hello", "message"); err == nil {
		t.Fatal("expected missing teammate error")
	}
	if _, err := os.Stat(filepath.Join(shared.TeamInboxDir(), "missing.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("missing teammate inbox should not be created, stat err: %v", err)
	}
}

func TestTeammateUnknownToolStillReturnsPairedToolResult(t *testing.T) {
	mock := &subagentMockLLM{
		responses: []string{
			buildToolCallResp(t, "call_unknown_1", "not_a_real_tool", `{}`),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()
	cl, err := client.Init(srv.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	mgr := newTestTeamManager(t)
	mgr.call = client.NewCall(cl, client.NewChatCompletionMessage(), nil)
	mgr.model = "test-model"

	messages := []any{*client.NewChatCompletionMessage().NewUserMessage("try unknown tool")}
	next, keepGoing, _, err := mgr.defaultRunTurn(context.Background(), "alice", "coder", messages)
	if err != nil {
		t.Fatal(err)
	}
	if !keepGoing {
		t.Fatal("tool call turn should continue")
	}
	var sawAssistantCall, sawToolResult bool
	for _, msg := range next {
		switch m := msg.(type) {
		case client.ResponseMessage:
			if len(m.ToolCalls) == 1 && m.ToolCalls[0].Id == "call_unknown_1" {
				sawAssistantCall = true
			}
		case client.ToolsMessage:
			if m.ToolsId == "call_unknown_1" && strings.Contains(m.Content, "unknown teammate tool") {
				sawToolResult = true
			}
		}
	}
	if !sawAssistantCall || !sawToolResult {
		t.Fatalf("expected paired assistant tool_call and tool result, messages: %#v", next)
	}
}

func TestTeammateRunTurnRetriesRateLimit(t *testing.T) {
	fastTeamRetries(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(buildFinalResp(t, "retried ok")))
	}))
	defer srv.Close()
	cl, err := client.Init(srv.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	mgr := newTestTeamManager(t)
	mgr.call = client.NewCall(cl, client.NewChatCompletionMessage(), nil)
	mgr.model = "test-model"

	messages := []any{*client.NewChatCompletionMessage().NewUserMessage("handle rate limit")}
	next, keepGoing, finalText, err := mgr.defaultRunTurn(context.Background(), "alice", "coder", messages)
	if err != nil {
		t.Fatal(err)
	}
	if keepGoing {
		t.Fatal("final response should stop the teammate turn")
	}
	if finalText != "retried ok" {
		t.Fatalf("finalText = %q", finalText)
	}
	if len(next) != 2 {
		t.Fatalf("messages = %d, want 2", len(next))
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestFormatTeamInbox(t *testing.T) {
	text := FormatTeamInbox([]MessageEnvelope{{Type: "message", From: "alice", To: "lead", Content: "ready"}})
	if !strings.Contains(text, "<team-inbox>") || !strings.Contains(text, "ready") {
		t.Fatalf("unexpected formatted inbox: %s", text)
	}
}
