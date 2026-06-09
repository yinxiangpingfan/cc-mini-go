package planning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
)

// setupTestTaskManager 创建临时目录，覆盖 shared.SessionStorageDir 以隔离测试，返回 TaskManager。
func setupTestTaskManager(t *testing.T) *TaskManager {
	t.Helper()
	old := shared.SessionStorageDir
	shared.SessionStorageDir = t.TempDir()
	t.Cleanup(func() { shared.SessionStorageDir = old })
	return NewTaskManager()
}

func TestTaskCreate(t *testing.T) {
	mgr := setupTestTaskManager(t)

	result, err := mgr.Create("Write parser", "Build the core parser module")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var task TaskRecord
	if err := json.Unmarshal([]byte(result), &task); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if task.ID != 1 {
		t.Errorf("expected ID=1, got %d", task.ID)
	}
	if task.Subject != "Write parser" {
		t.Errorf("expected subject 'Write parser', got '%s'", task.Subject)
	}
	if task.Status != TaskPending {
		t.Errorf("expected status pending, got %s", task.Status)
	}

	// 验证文件落盘
	path := filepath.Join(mgr.dir, "task_1.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("task file not persisted to disk")
	}
}

func TestTaskCreateIncrementID(t *testing.T) {
	mgr := setupTestTaskManager(t)

	r1, _ := mgr.Create("Task A", "")
	r2, _ := mgr.Create("Task B", "")

	var t1, t2 TaskRecord
	json.Unmarshal([]byte(r1), &t1)
	json.Unmarshal([]byte(r2), &t2)

	if t1.ID != 1 || t2.ID != 2 {
		t.Errorf("expected IDs 1,2 got %d,%d", t1.ID, t2.ID)
	}
}

func TestTaskGet(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Test task", "")

	result, err := mgr.Get(1)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var task TaskRecord
	json.Unmarshal([]byte(result), &task)
	if task.Subject != "Test task" {
		t.Errorf("expected 'Test task', got '%s'", task.Subject)
	}
}

func TestTaskGetNotFound(t *testing.T) {
	mgr := setupTestTaskManager(t)

	_, err := mgr.Get(999)
	if err == nil {
		t.Error("expected error for non-existent task")
	}
}

func TestTaskUpdateStatus(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("My task", "")

	result, err := mgr.Update(1, "in_progress", "", nil, nil)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var task TaskRecord
	json.Unmarshal([]byte(result), &task)
	if task.Status != TaskInProgress {
		t.Errorf("expected in_progress, got %s", task.Status)
	}
}

func TestTaskUpdateInvalidStatus(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("My task", "")

	_, err := mgr.Update(1, "bogus", "", nil, nil)
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestTaskUpdateOwner(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("My task", "")

	result, err := mgr.Update(1, "", "agent-1", nil, nil)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	var task TaskRecord
	json.Unmarshal([]byte(result), &task)
	if task.Owner != "agent-1" {
		t.Errorf("expected owner 'agent-1', got '%s'", task.Owner)
	}
}

func TestAddBlocksBidirectional(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Task A", "")
	mgr.Create("Task B", "")

	// Task A blocks Task B
	mgr.Update(1, "", "", nil, []int{2})

	// 验证 Task A 的 blocks 包含 2
	r1, _ := mgr.Get(1)
	var t1 TaskRecord
	json.Unmarshal([]byte(r1), &t1)
	if !containsInt(t1.Blocks, 2) {
		t.Error("Task A should have Task B in blocks")
	}

	// 验证 Task B 的 blockedBy 包含 1（双向自动维护）
	r2, _ := mgr.Get(2)
	var t2 TaskRecord
	json.Unmarshal([]byte(r2), &t2)
	if !containsInt(t2.BlockedBy, 1) {
		t.Error("Task B should have Task A in blockedBy (bidirectional)")
	}
}

func TestCompleteUnblocks(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Task A", "")
	mgr.Create("Task B", "")

	// Task A blocks Task B
	mgr.Update(1, "", "", nil, []int{2})

	// 完成 Task A
	mgr.Update(1, "completed", "", nil, nil)

	// Task B 应该不再被阻塞
	r2, _ := mgr.Get(2)
	var t2 TaskRecord
	json.Unmarshal([]byte(r2), &t2)
	if len(t2.BlockedBy) != 0 {
		t.Errorf("Task B should be unblocked after Task A completed, got blockedBy=%v", t2.BlockedBy)
	}
}

func TestCompleteUnblocksChain(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Task A", "") // 1
	mgr.Create("Task B", "") // 2
	mgr.Create("Task C", "") // 3

	// A -> B -> C
	mgr.Update(1, "", "", nil, []int{2})
	mgr.Update(2, "", "", nil, []int{3})

	// 完成 A：只解锁 B，C 仍被 B 阻塞
	mgr.Update(1, "completed", "", nil, nil)

	r2, _ := mgr.Get(2)
	var t2 TaskRecord
	json.Unmarshal([]byte(r2), &t2)
	if len(t2.BlockedBy) != 0 {
		t.Errorf("Task B should be unblocked, got blockedBy=%v", t2.BlockedBy)
	}

	r3, _ := mgr.Get(3)
	var t3 TaskRecord
	json.Unmarshal([]byte(r3), &t3)
	if !containsInt(t3.BlockedBy, 2) {
		t.Error("Task C should still be blocked by Task B")
	}

	// 完成 B：解锁 C
	mgr.Update(2, "completed", "", nil, nil)

	r3, _ = mgr.Get(3)
	json.Unmarshal([]byte(r3), &t3)
	if len(t3.BlockedBy) != 0 {
		t.Errorf("Task C should be unblocked after B completed, got blockedBy=%v", t3.BlockedBy)
	}
}

func TestListAll(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Task A", "")
	mgr.Create("Task B", "")
	mgr.Update(1, "in_progress", "alice", nil, []int{2})

	result := mgr.ListAll()

	if result == "No tasks." {
		t.Fatal("expected tasks in list")
	}
	// 检查格式化输出包含预期内容
	if !strings.Contains(result, "[>] #1: Task A") {
		t.Errorf("expected in_progress marker for Task A, got:\n%s", result)
	}
	if !strings.Contains(result, "owner=alice") {
		t.Errorf("expected owner=alice, got:\n%s", result)
	}
	if !strings.Contains(result, "(blocked by: [1])") {
		t.Errorf("expected blocked by info for Task B, got:\n%s", result)
	}
}

func TestListAllEmpty(t *testing.T) {
	mgr := setupTestTaskManager(t)

	result := mgr.ListAll()
	if result != "No tasks." {
		t.Errorf("expected 'No tasks.', got '%s'", result)
	}
}

func TestAddBlockedBy(t *testing.T) {
	mgr := setupTestTaskManager(t)
	mgr.Create("Task A", "")
	mgr.Create("Task B", "")

	mgr.Update(2, "", "", []int{1}, nil)

	r2, _ := mgr.Get(2)
	var t2 TaskRecord
	json.Unmarshal([]byte(r2), &t2)
	if !containsInt(t2.BlockedBy, 1) {
		t.Error("Task B should have Task A in blockedBy")
	}
}

func TestMaxIDRestoredFromDisk(t *testing.T) {
	old := shared.SessionStorageDir
	tmpDir := t.TempDir()
	shared.SessionStorageDir = tmpDir
	t.Cleanup(func() { shared.SessionStorageDir = old })

	// 第一个 manager 创建 2 个任务
	mgr1 := NewTaskManager()
	mgr1.Create("A", "")
	mgr1.Create("B", "")

	// 第二个 manager 从同一目录恢复
	mgr2 := NewTaskManager()
	r, _ := mgr2.Create("C", "")

	var task TaskRecord
	json.Unmarshal([]byte(r), &task)
	if task.ID != 3 {
		t.Errorf("expected ID=3 after restore, got %d", task.ID)
	}
}
