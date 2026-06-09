package planning

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
)

// resetTodoList 每个测试前重置全局 ToDoList，避免测试间互相干扰
func resetTodoList(t *testing.T) {
	t.Helper()
	shared.ToDoList.MU.Lock()
	shared.ToDoList.Items = []shared.PlanItem{}
	shared.ToDoList.RoundsSinceUpdate = 0
	shared.ToDoList.MU.Unlock()
}

func TestUpdateTodoList_BasicUpdate(t *testing.T) {
	resetTodoList(t)
	items := []shared.TodoItem{
		{Subject: "买菜", Status: shared.StatusPending},
		{Subject: "写代码", Status: shared.StatusInProgress},
		{Subject: "健身", Status: shared.StatusPending},
	}

	if err := updateTodoList(items); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if len(shared.ToDoList.Items) != 3 {
		t.Fatalf("expected 3 items, got: %d", len(shared.ToDoList.Items))
	}
	if shared.ToDoList.Items[0].TodoItem.Subject != "买菜" {
		t.Fatalf("expected first item '买菜', got: %s", shared.ToDoList.Items[0].TodoItem.Subject)
	}
	if shared.ToDoList.Items[1].TodoItem.Status != shared.StatusInProgress {
		t.Fatalf("expected second item in_progress, got: %s", shared.ToDoList.Items[1].TodoItem.Status)
	}
}

func TestUpdateTodoList_MultipleInProgressRejected(t *testing.T) {
	resetTodoList(t)
	items := []shared.TodoItem{
		{Subject: "任务A", Status: shared.StatusInProgress},
		{Subject: "任务B", Status: shared.StatusInProgress},
	}

	err := updateTodoList(items)
	if err == nil {
		t.Fatal("expected error when more than one item is in_progress")
	}
	if !strings.Contains(err.Error(), "only one item can be in_progress") {
		t.Fatalf("expected in_progress constraint error, got: %v", err)
	}

	// 验证状态未被污染（仍为空）
	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if len(shared.ToDoList.Items) != 0 {
		t.Fatalf("expected state unchanged on rejected update, got %d items", len(shared.ToDoList.Items))
	}
}

func TestUpdateTodoList_SingleInProgressAllowed(t *testing.T) {
	resetTodoList(t)
	items := []shared.TodoItem{
		{Subject: "完成的任务", Status: shared.StatusCompleted},
		{Subject: "进行中任务", Status: shared.StatusInProgress},
		{Subject: "待办任务", Status: shared.StatusPending},
	}

	if err := updateTodoList(items); err != nil {
		t.Fatalf("expected no error with single in_progress, got: %v", err)
	}
}

func TestUpdateTodoList_EmptyList(t *testing.T) {
	resetTodoList(t)
	if err := updateTodoList([]shared.TodoItem{}); err != nil {
		t.Fatalf("expected no error for empty list, got: %v", err)
	}

	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if len(shared.ToDoList.Items) != 0 {
		t.Fatalf("expected 0 items, got: %d", len(shared.ToDoList.Items))
	}
}

func TestUpdateTodoList_ResetsRoundsSinceUpdate(t *testing.T) {
	resetTodoList(t)
	// 模拟已经过去了几轮
	shared.ToDoList.MU.Lock()
	shared.ToDoList.RoundsSinceUpdate = 5
	shared.ToDoList.MU.Unlock()

	if err := updateTodoList([]shared.TodoItem{{Subject: "新计划", Status: shared.StatusPending}}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if shared.ToDoList.RoundsSinceUpdate != 0 {
		t.Fatalf("expected RoundsSinceUpdate reset to 0, got: %d", shared.ToDoList.RoundsSinceUpdate)
	}
}

func TestUpdateTodoList_ReplacesPreviousPlan(t *testing.T) {
	resetTodoList(t)
	// 第一次更新
	if err := updateTodoList([]shared.TodoItem{
		{Subject: "旧任务1", Status: shared.StatusPending},
		{Subject: "旧任务2", Status: shared.StatusPending},
	}); err != nil {
		t.Fatalf("first update error: %v", err)
	}

	// 第二次整份重写
	if err := updateTodoList([]shared.TodoItem{
		{Subject: "新任务", Status: shared.StatusInProgress},
	}); err != nil {
		t.Fatalf("second update error: %v", err)
	}

	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if len(shared.ToDoList.Items) != 1 {
		t.Fatalf("expected plan replaced with 1 item, got: %d", len(shared.ToDoList.Items))
	}
	if shared.ToDoList.Items[0].TodoItem.Subject != "新任务" {
		t.Fatalf("expected '新任务', got: %s", shared.ToDoList.Items[0].TodoItem.Subject)
	}
}

// ---------- 工具入口 NewTodoListTool().Func 测试 ----------

func TestTodoListTool_MissingTodosArg(t *testing.T) {
	resetTodoList(t)
	tool := NewTodoListTool()
	out := tool.Func(context.Background(), map[string]interface{}{})

	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error response when 'todos' is missing, got: %s", out)
	}
}

func TestTodoListTool_ValidTodos(t *testing.T) {
	resetTodoList(t)
	tool := NewTodoListTool()
	out := tool.Func(context.Background(), map[string]interface{}{
		"todos": []map[string]interface{}{
			{"subject": "买菜", "status": "pending"},
			{"subject": "写代码", "status": "in_progress"},
			{"subject": "健身", "status": "completed"},
		},
	})

	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("expected success, got error response: %s", out)
	}
	if _, ok := resp["todos"]; !ok {
		t.Fatalf("expected 'todos' in response, got: %s", out)
	}

	// 验证全局状态已被更新
	shared.ToDoList.MU.RLock()
	defer shared.ToDoList.MU.RUnlock()
	if len(shared.ToDoList.Items) != 3 {
		t.Fatalf("expected 3 items in global state, got: %d", len(shared.ToDoList.Items))
	}
}

func TestTodoListTool_MultipleInProgressReturnsError(t *testing.T) {
	resetTodoList(t)
	tool := NewTodoListTool()
	out := tool.Func(context.Background(), map[string]interface{}{
		"todos": []map[string]interface{}{
			{"subject": "任务A", "status": "in_progress"},
			{"subject": "任务B", "status": "in_progress"},
		},
	})

	var resp map[string]string
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("output is not valid JSON: %v, raw: %s", err, out)
	}
	msg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error response, got: %s", out)
	}
	if !strings.Contains(msg, "only one item can be in_progress") {
		t.Fatalf("expected in_progress constraint error, got: %s", msg)
	}
}

func TestTodoListTool_OutputIsValidJSON(t *testing.T) {
	resetTodoList(t)
	tool := NewTodoListTool()
	out := tool.Func(context.Background(), map[string]interface{}{
		"todos": []map[string]interface{}{
			{"subject": "单个任务", "status": "pending"},
		},
	})

	if !json.Valid([]byte(out)) {
		t.Fatalf("tool output is not valid JSON: %s", out)
	}
}
