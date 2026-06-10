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

func withCronTempStorage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldProject := shared.ProjectStorageDir
	oldSession := shared.SessionStorageDir
	shared.ProjectStorageDir = dir
	shared.SessionStorageDir = filepath.Join(dir, "session")
	t.Cleanup(func() {
		shared.ProjectStorageDir = oldProject
		shared.SessionStorageDir = oldSession
	})
	return dir
}

func newTestCronScheduler() *CronScheduler {
	return &CronScheduler{
		tasksFile:       shared.ScheduledTasksFile(),
		lock:            NewCronLock(shared.CronLockFile()),
		checkInterval:   time.Hour,
		now:             time.Now,
		stop:            make(chan struct{}),
		lastCheckMinute: -1,
		ownsDurableLock: true,
	}
}

func TestCronMatches(t *testing.T) {
	dt := time.Date(2026, 6, 7, 14, 30, 0, 0, time.Local) // Sunday
	cases := []struct {
		expr string
		want bool
	}{
		{"30 14 * * *", true},
		{"*/5 * * * *", true},
		{"29 14 * * *", false},
		{"30 9-17 * * *", true},
		{"15,30,45 14 * * *", true},
		{"30 14 * * 0", true},
		{"30 14 * * 1", false},
		{"bad", false},
	}
	for _, tc := range cases {
		if got := CronMatches(tc.expr, dt); got != tc.want {
			t.Fatalf("CronMatches(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestCronCreatePersistenceModes(t *testing.T) {
	withCronTempStorage(t)
	s := newTestCronScheduler()

	sessionTask, err := s.Create("* * * * *", "session prompt", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if sessionTask.Durable {
		t.Fatal("session task should not be durable")
	}
	if _, err := os.Stat(shared.ScheduledTasksFile()); !os.IsNotExist(err) {
		t.Fatalf("session-only task should not write durable file, stat err=%v", err)
	}

	durableTask, err := s.Create("30 14 * * *", "durable prompt", false, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(shared.ScheduledTasksFile())
	if err != nil {
		t.Fatal(err)
	}
	var records []CronTaskRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != durableTask.ID {
		t.Fatalf("durable records = %#v, want only %s", records, durableTask.ID)
	}

	restored := newTestCronScheduler()
	restored.loadDurableLocked()
	if len(restored.tasks) != 1 || restored.tasks[0].ID != durableTask.ID {
		t.Fatalf("restored tasks = %#v", restored.tasks)
	}
}

func TestCronOneShotFiresOnceAndDeletes(t *testing.T) {
	withCronTempStorage(t)
	s := newTestCronScheduler()
	now := time.Date(2026, 6, 7, 14, 30, 0, 0, time.Local)
	task, err := s.Create("30 14 * * *", "run once", false, true)
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.checkTasksLocked(now)
	s.mu.Unlock()

	notifs := s.DrainNotifications()
	if len(notifs) != 1 || !strings.Contains(notifs[0], task.ID) {
		t.Fatalf("notifications = %#v", notifs)
	}
	if strings.Contains(s.ListTasks(), task.ID) {
		t.Fatalf("one-shot task should be deleted after firing: %s", s.ListTasks())
	}
}

func TestCronDoesNotDoubleFireSameMinute(t *testing.T) {
	withCronTempStorage(t)
	s := newTestCronScheduler()
	now := time.Date(2026, 6, 7, 14, 31, 0, 0, time.Local)
	if _, err := s.Create("31 14 * * *", "once per minute", true, false); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.checkTasksLocked(now)
	s.checkTasksLocked(now.Add(20 * time.Second))
	s.mu.Unlock()

	if got := len(s.DrainNotifications()); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
}

func TestCronRecurringAutoExpires(t *testing.T) {
	withCronTempStorage(t)
	s := newTestCronScheduler()
	now := time.Date(2026, 6, 7, 14, 31, 0, 0, time.Local)
	task, err := s.Create("31 14 * * *", "old task", true, false)
	if err != nil {
		t.Fatal(err)
	}
	s.tasks[0].CreatedAt = unixSeconds(now.AddDate(0, 0, -8))

	s.mu.Lock()
	s.checkTasksLocked(now)
	s.mu.Unlock()

	if strings.Contains(s.ListTasks(), task.ID) {
		t.Fatalf("expired recurring task should be removed: %s", s.ListTasks())
	}
}

func TestCronDelete(t *testing.T) {
	withCronTempStorage(t)
	s := newTestCronScheduler()
	task, err := s.Create("* * * * *", "delete me", true, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Delete(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, task.ID) {
		t.Fatalf("delete output = %q", out)
	}
	if !strings.Contains(s.ListTasks(), "No scheduled tasks") {
		t.Fatalf("task should be gone: %s", s.ListTasks())
	}
	data, err := os.ReadFile(shared.ScheduledTasksFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), task.ID) {
		t.Fatalf("durable file still contains deleted task: %s", data)
	}
}

func TestCronDetectMissedDurableTaskOnStart(t *testing.T) {
	withCronTempStorage(t)
	now := time.Date(2026, 6, 7, 13, 0, 0, 0, time.Local)
	task := CronTaskRecord{
		ID:        "missed1",
		Cron:      "0 12 * * *",
		Prompt:    "check noon result",
		Recurring: true,
		Durable:   true,
		CreatedAt: unixSeconds(now.Add(-2 * time.Hour)),
	}
	data, _ := json.MarshalIndent([]CronTaskRecord{task}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(shared.ScheduledTasksFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared.ScheduledTasksFile(), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewCronScheduler()
	s.checkInterval = time.Hour
	s.now = func() time.Time { return now }
	s.Start()
	t.Cleanup(s.Stop)

	notifs := s.DrainNotifications()
	if len(notifs) != 1 || !strings.Contains(notifs[0], "Missed scheduled task missed1") {
		t.Fatalf("notifications = %#v", notifs)
	}
	restored := newTestCronScheduler()
	restored.loadDurableLocked()
	if len(restored.tasks) != 1 || restored.tasks[0].LastFired == nil {
		t.Fatalf("missed recurring task should persist with last_fired: %#v", restored.tasks)
	}
}

func TestCronMissedOneShotDeletesAfterNotification(t *testing.T) {
	withCronTempStorage(t)
	now := time.Date(2026, 6, 7, 13, 0, 0, 0, time.Local)
	task := CronTaskRecord{
		ID:        "oneshot1",
		Cron:      "0 12 * * *",
		Prompt:    "one shot noon",
		Recurring: false,
		Durable:   true,
		CreatedAt: unixSeconds(now.Add(-2 * time.Hour)),
	}
	data, _ := json.MarshalIndent([]CronTaskRecord{task}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(shared.ScheduledTasksFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared.ScheduledTasksFile(), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewCronScheduler()
	s.checkInterval = time.Hour
	s.now = func() time.Time { return now }
	s.Start()
	t.Cleanup(s.Stop)

	if got := len(s.DrainNotifications()); got != 1 {
		t.Fatalf("notifications = %d, want 1", got)
	}
	if strings.Contains(s.ListTasks(), task.ID) {
		t.Fatalf("missed one-shot should be removed: %s", s.ListTasks())
	}
	fileData, err := os.ReadFile(shared.ScheduledTasksFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(fileData), task.ID) {
		t.Fatalf("durable file still contains missed one-shot: %s", fileData)
	}
}

func TestCronMissedDurableRequiresLockOwner(t *testing.T) {
	withCronTempStorage(t)
	now := time.Date(2026, 6, 7, 13, 0, 0, 0, time.Local)
	holder := NewCronLock(shared.CronLockFile())
	if !holder.Acquire() {
		t.Fatal("expected holder to acquire cron lock")
	}
	t.Cleanup(holder.Release)
	task := CronTaskRecord{
		ID:        "locked1",
		Cron:      "0 12 * * *",
		Prompt:    "locked durable",
		Recurring: true,
		Durable:   true,
		CreatedAt: unixSeconds(now.Add(-2 * time.Hour)),
	}
	data, _ := json.MarshalIndent([]CronTaskRecord{task}, "", "  ")
	if err := os.MkdirAll(filepath.Dir(shared.ScheduledTasksFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared.ScheduledTasksFile(), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewCronScheduler()
	s.checkInterval = time.Hour
	s.now = func() time.Time { return now }
	s.Start()
	t.Cleanup(s.Stop)

	if got := len(s.DrainNotifications()); got != 0 {
		t.Fatalf("notifications = %d, want 0", got)
	}
	if s.ownsDurableLock {
		t.Fatal("scheduler should not own durable lock")
	}
}

func TestCronLockAcquireAndRelease(t *testing.T) {
	dir := withCronTempStorage(t)
	lockPath := filepath.Join(dir, "cron.lock")
	lock := NewCronLock(lockPath)
	if !lock.Acquire() {
		t.Fatal("expected lock acquire")
	}
	if NewCronLock(lockPath).Acquire() {
		t.Fatal("second live process lock should not acquire")
	}
	lock.Release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock should be released, stat err=%v", err)
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("99999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NewCronLock(lockPath).Acquire() {
		t.Fatal("stale lock should be acquired")
	}
}

func TestFormatCronNotifications(t *testing.T) {
	text := FormatCronNotifications([]string{"[Scheduled task abc123]: do it"})
	if !strings.Contains(text, "<scheduled-prompts>") || !strings.Contains(text, "[Scheduled task abc123]: do it") {
		t.Fatalf("unexpected notification text: %s", text)
	}
}
