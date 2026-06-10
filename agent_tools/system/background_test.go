package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
)

func withBackgroundTempDir(t *testing.T) {
	t.Helper()
	old := shared.SessionStorageDir
	shared.SessionStorageDir = t.TempDir()
	t.Cleanup(func() { shared.SessionStorageDir = old })
}

func waitForBackground(t *testing.T, mgr *BackgroundManager, taskID string, want BackgroundStatus) RuntimeTaskRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.RLock()
		task := *mgr.tasks[taskID]
		mgr.mu.RUnlock()
		if task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background task %s did not reach %s", taskID, want)
	return RuntimeTaskRecord{}
}

func TestBackgroundRunCompletesAndDrainsNotification(t *testing.T) {
	withBackgroundTempDir(t)
	mgr := NewBackgroundManager()

	task, err := mgr.Run("printf 'hello background'", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != BackgroundRunning {
		t.Fatalf("status = %s, want running", task.Status)
	}

	done := waitForBackground(t, mgr, task.ID, BackgroundCompleted)
	if done.ResultPreview != "hello background" {
		t.Fatalf("preview = %q", done.ResultPreview)
	}
	data, err := os.ReadFile(done.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello background" {
		t.Fatalf("log = %q", string(data))
	}

	recordData, err := os.ReadFile(filepath.Join(shared.RuntimeTasksDir(), task.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var record RuntimeTaskRecord
	if err := json.Unmarshal(recordData, &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != BackgroundCompleted {
		t.Fatalf("persisted status = %s", record.Status)
	}

	notifs := mgr.DrainNotifications()
	if len(notifs) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifs))
	}
	if len(mgr.DrainNotifications()) != 0 {
		t.Fatal("notifications should drain only once")
	}
}

func TestBackgroundRunTimeout(t *testing.T) {
	withBackgroundTempDir(t)
	mgr := NewBackgroundManager()

	task, err := mgr.Run("sleep 2", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	done := waitForBackground(t, mgr, task.ID, BackgroundTimeout)
	if !strings.Contains(done.ResultPreview, "Timeout") {
		t.Fatalf("preview = %q, want timeout", done.ResultPreview)
	}
}

func TestBackgroundCheckOneAndAll(t *testing.T) {
	withBackgroundTempDir(t)
	mgr := NewBackgroundManager()

	task, err := mgr.Run("printf checked", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForBackground(t, mgr, task.ID, BackgroundCompleted)

	one, err := mgr.Check(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(one, `"id": "`+task.ID+`"`) {
		t.Fatalf("single check missing id: %s", one)
	}

	all, err := mgr.Check("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, task.ID+": [completed]") {
		t.Fatalf("list check missing task: %s", all)
	}
}

func TestFormatBackgroundNotifications(t *testing.T) {
	text := FormatBackgroundNotifications([]BackgroundNotification{{
		TaskID:     "abc123",
		Status:     BackgroundCompleted,
		Preview:    "tests passed",
		OutputFile: "/tmp/abc123.log",
	}})
	if !strings.Contains(text, "<background-results>") || !strings.Contains(text, "[bg:abc123] completed: tests passed") {
		t.Fatalf("unexpected notification text: %s", text)
	}
}
