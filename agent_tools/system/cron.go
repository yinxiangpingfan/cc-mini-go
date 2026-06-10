package system

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

const (
	cronAutoExpiryDays = 7
	cronPreviewChars   = 60
	cronCheckInterval  = time.Second
)

var cronJitterMinutes = map[int]bool{0: true, 30: true}

type CronTaskRecord struct {
	ID           string   `json:"id"`
	Cron         string   `json:"cron"`
	Prompt       string   `json:"prompt"`
	Recurring    bool     `json:"recurring"`
	Durable      bool     `json:"durable"`
	CreatedAt    float64  `json:"createdAt"`
	LastFired    *float64 `json:"last_fired,omitempty"`
	JitterOffset int      `json:"jitter_offset,omitempty"`
}

type CronLock struct {
	path string
}

func NewCronLock(path string) *CronLock {
	if path == "" {
		path = shared.CronLockFile()
	}
	return &CronLock{path: path}
}

func (l *CronLock) Acquire() bool {
	if l.path == "" {
		return false
	}
	if data, err := os.ReadFile(l.path); err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && processAlive(pid) {
			return false
		}
		_ = os.Remove(l.path)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(l.path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return false
	}
	return true
}

func (l *CronLock) Release() {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err == nil && pid == os.Getpid() {
		_ = os.Remove(l.path)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

type CronScheduler struct {
	tasks           []CronTaskRecord
	notifications   []string
	tasksFile       string
	lock            *CronLock
	ownsDurableLock bool
	checkInterval   time.Duration
	now             func() time.Time
	stop            chan struct{}
	started         bool
	lastCheckMinute int
	mu              sync.Mutex
}

func NewCronScheduler() *CronScheduler {
	return &CronScheduler{
		tasksFile:       shared.ScheduledTasksFile(),
		lock:            NewCronLock(shared.CronLockFile()),
		checkInterval:   cronCheckInterval,
		now:             time.Now,
		stop:            make(chan struct{}),
		lastCheckMinute: -1,
	}
}

func (s *CronScheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.loadDurableLocked()
	s.ownsDurableLock = s.lock == nil || s.lock.Acquire()
	if s.ownsDurableLock {
		s.detectMissedTasksLocked(s.now())
	}
	interval := s.checkInterval
	s.mu.Unlock()

	go s.checkLoop(interval)
}

func (s *CronScheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	close(s.stop)
	s.started = false
	if s.ownsDurableLock && s.lock != nil {
		s.lock.Release()
	}
	s.mu.Unlock()
}

func (s *CronScheduler) Create(cronExpr, taskPrompt string, recurring, durable bool) (CronTaskRecord, error) {
	cronExpr = strings.TrimSpace(cronExpr)
	taskPrompt = strings.TrimSpace(taskPrompt)
	if taskPrompt == "" {
		return CronTaskRecord{}, fmt.Errorf(errors.ErrToolFunctionCall, "prompt")
	}
	if err := validateCron(cronExpr); err != nil {
		return CronTaskRecord{}, err
	}

	task := CronTaskRecord{
		ID:           newBackgroundTaskID(),
		Cron:         cronExpr,
		Prompt:       taskPrompt,
		Recurring:    recurring,
		Durable:      durable,
		CreatedAt:    unixSeconds(time.Now()),
		JitterOffset: 0,
	}
	if recurring {
		task.JitterOffset = computeCronJitter(cronExpr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	if durable {
		if err := s.saveDurableLocked(); err != nil {
			return CronTaskRecord{}, err
		}
	}
	return task, nil
}

func (s *CronScheduler) Delete(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf(errors.ErrToolFunctionCall, "id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.tasks)
	filtered := s.tasks[:0]
	for _, task := range s.tasks {
		if task.ID != taskID {
			filtered = append(filtered, task)
		}
	}
	s.tasks = filtered
	if len(s.tasks) == before {
		return "", fmt.Errorf(errors.ErrCronTaskNotFound, taskID)
	}
	if err := s.saveDurableLocked(); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted task %s", taskID), nil
}

func (s *CronScheduler) ListTasks() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 {
		return "No scheduled tasks."
	}
	tasks := append([]CronTaskRecord(nil), s.tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt < tasks[j].CreatedAt })
	lines := make([]string, 0, len(tasks))
	now := time.Now()
	for _, task := range tasks {
		mode := "one-shot"
		if task.Recurring {
			mode = "recurring"
		}
		store := "session"
		if task.Durable {
			store = "durable"
		}
		ageHours := (unixSeconds(now) - task.CreatedAt) / 3600
		lines = append(lines, fmt.Sprintf("  %s  %s  [%s/%s] (%.1fh old): %s",
			task.ID, task.Cron, mode, store, ageHours, truncateRunes(task.Prompt, cronPreviewChars)))
	}
	return strings.Join(lines, "\n")
}

func (s *CronScheduler) DrainNotifications() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	notifs := append([]string(nil), s.notifications...)
	s.notifications = nil
	return notifs
}

func (s *CronScheduler) CheckNow(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkTasksLocked(now)
}

func (s *CronScheduler) checkLoop(interval time.Duration) {
	if interval <= 0 {
		interval = cronCheckInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := s.now()
			currentMinute := now.Hour()*60 + now.Minute()
			s.mu.Lock()
			if currentMinute != s.lastCheckMinute {
				s.lastCheckMinute = currentMinute
				s.checkTasksLocked(now)
			}
			s.mu.Unlock()
		}
	}
}

func (s *CronScheduler) checkTasksLocked(now time.Time) {
	removeIDs := map[string]bool{}
	dirtyDurable := false
	nowUnix := unixSeconds(now)
	for i := range s.tasks {
		task := &s.tasks[i]
		if task.Durable && !s.ownsDurableLock {
			continue
		}
		ageDays := (nowUnix - task.CreatedAt) / 86400
		if task.Recurring && ageDays > cronAutoExpiryDays {
			removeIDs[task.ID] = true
			if task.Durable {
				dirtyDurable = true
			}
			continue
		}

		if !taskDueAt(*task, now) || firedInSameMinute(task.LastFired, now) {
			continue
		}
		s.notifications = append(s.notifications, formatScheduledNotification(*task))
		firedAt := nowUnix
		task.LastFired = &firedAt
		if task.Durable {
			dirtyDurable = true
		}
		if !task.Recurring {
			removeIDs[task.ID] = true
		}
	}
	if len(removeIDs) == 0 {
		if dirtyDurable {
			_ = s.saveDurableLocked()
		}
		return
	}
	filtered := s.tasks[:0]
	for _, task := range s.tasks {
		if !removeIDs[task.ID] {
			filtered = append(filtered, task)
		} else if task.Durable {
			dirtyDurable = true
		}
	}
	s.tasks = filtered
	if dirtyDurable {
		_ = s.saveDurableLocked()
	}
}

func (s *CronScheduler) detectMissedTasksLocked(now time.Time) {
	removeIDs := map[string]bool{}
	dirtyDurable := false
	nowUnix := unixSeconds(now)
	for i := range s.tasks {
		task := &s.tasks[i]
		if !task.Durable {
			continue
		}
		ageDays := (nowUnix - task.CreatedAt) / 86400
		if task.Recurring && ageDays > cronAutoExpiryDays {
			removeIDs[task.ID] = true
			dirtyDurable = true
			continue
		}
		missedAt, ok := missedTaskTime(*task, now)
		if !ok {
			continue
		}
		s.notifications = append(s.notifications, formatMissedScheduledNotification(*task, missedAt))
		firedAt := nowUnix
		task.LastFired = &firedAt
		dirtyDurable = true
		if !task.Recurring {
			removeIDs[task.ID] = true
		}
	}
	if len(removeIDs) > 0 {
		filtered := s.tasks[:0]
		for _, task := range s.tasks {
			if !removeIDs[task.ID] {
				filtered = append(filtered, task)
			}
		}
		s.tasks = filtered
	}
	if dirtyDurable {
		_ = s.saveDurableLocked()
	}
}

func missedTaskTime(task CronTaskRecord, now time.Time) (time.Time, bool) {
	refUnix := task.CreatedAt
	if task.LastFired != nil {
		refUnix = *task.LastFired
	}
	start := time.Unix(0, int64(refUnix*float64(time.Second))).Truncate(time.Minute).Add(time.Minute)
	if start.After(now) {
		return time.Time{}, false
	}
	capTime := start.Add(24 * time.Hour)
	if now.Before(capTime) {
		capTime = now
	}
	for check := start; !check.After(capTime); check = check.Add(time.Minute) {
		if taskDueAt(task, check) {
			return check, true
		}
	}
	return time.Time{}, false
}

func taskDueAt(task CronTaskRecord, now time.Time) bool {
	checkTime := now
	if task.JitterOffset > 0 {
		checkTime = now.Add(-time.Duration(task.JitterOffset) * time.Minute)
	}
	return CronMatches(task.Cron, checkTime)
}

func formatScheduledNotification(task CronTaskRecord) string {
	return fmt.Sprintf("[Scheduled task %s]: %s", task.ID, task.Prompt)
}

func formatMissedScheduledNotification(task CronTaskRecord, missedAt time.Time) string {
	return fmt.Sprintf("[Missed scheduled task %s at %s]: %s",
		task.ID, missedAt.Format("2006-01-02 15:04"), task.Prompt)
}

func firedInSameMinute(last *float64, now time.Time) bool {
	if last == nil {
		return false
	}
	lastTime := time.Unix(0, int64(*last*float64(time.Second)))
	return lastTime.Year() == now.Year() && lastTime.YearDay() == now.YearDay() &&
		lastTime.Hour() == now.Hour() && lastTime.Minute() == now.Minute()
}

func (s *CronScheduler) loadDurableLocked() {
	data, err := os.ReadFile(s.tasksFile)
	if err != nil {
		return
	}
	var tasks []CronTaskRecord
	if err := json.Unmarshal(data, &tasks); err != nil {
		return
	}
	for _, task := range tasks {
		if task.Durable {
			s.tasks = append(s.tasks, task)
		}
	}
}

func (s *CronScheduler) saveDurableLocked() error {
	durable := make([]CronTaskRecord, 0)
	for _, task := range s.tasks {
		if task.Durable {
			durable = append(durable, task)
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.tasksFile), 0o755); err != nil {
		return fmt.Errorf("%w: %w", errors.ErrCronTaskWrite, err)
	}
	data, err := json.MarshalIndent(durable, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %w", errors.ErrCronTaskWrite, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(s.tasksFile, data, 0o644); err != nil {
		return fmt.Errorf("%w: %w", errors.ErrCronTaskWrite, err)
	}
	return nil
}

func FormatCronNotifications(notifs []string) string {
	if len(notifs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(notifs)+2)
	lines = append(lines, "<scheduled-prompts>")
	lines = append(lines, notifs...)
	lines = append(lines, "</scheduled-prompts>")
	return strings.Join(lines, "\n")
}

func computeCronJitter(cronExpr string) int {
	fields := strings.Fields(cronExpr)
	if len(fields) == 0 {
		return 0
	}
	minute, err := strconv.Atoi(fields[0])
	if err != nil || !cronJitterMinutes[minute] {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(cronExpr))
	return int(h.Sum32()%4) + 1
}

func validateCron(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf(errors.ErrCronInvalidExpr, expr)
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		if err := validateCronField(field, ranges[i][0], ranges[i][1]); err != nil {
			return fmt.Errorf("%s: %w", fmt.Sprintf(errors.ErrCronInvalidExpr, expr), err)
		}
	}
	return nil
}

func validateCronField(field string, lo, hi int) error {
	if field == "" {
		return fmt.Errorf("empty field")
	}
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return fmt.Errorf("empty list item")
		}
		base := part
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return fmt.Errorf("invalid step")
			}
			base = pieces[0]
			n, err := strconv.Atoi(pieces[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid step")
			}
			step = n
		}
		start, end, err := cronPartRange(base, lo, hi)
		if err != nil {
			return err
		}
		if start < lo || end > hi || start > end || step <= 0 {
			return fmt.Errorf("field out of range")
		}
	}
	return nil
}

func cronPartRange(part string, lo, hi int) (int, int, error) {
	switch {
	case part == "*":
		return lo, hi, nil
	case strings.Contains(part, "-"):
		pieces := strings.Split(part, "-")
		if len(pieces) != 2 {
			return 0, 0, fmt.Errorf("invalid range")
		}
		start, err1 := strconv.Atoi(pieces[0])
		end, err2 := strconv.Atoi(pieces[1])
		if err1 != nil || err2 != nil {
			return 0, 0, fmt.Errorf("invalid range")
		}
		return start, end, nil
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid value")
		}
		return n, n, nil
	}
}

func CronMatches(expr string, dt time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	values := []int{dt.Minute(), dt.Hour(), dt.Day(), int(dt.Month()), int(dt.Weekday())}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		ok, err := cronFieldMatches(field, values[i], ranges[i][0], ranges[i][1])
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func cronFieldMatches(field string, value, lo, hi int) (bool, error) {
	for _, part := range strings.Split(field, ",") {
		base := part
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return false, fmt.Errorf("invalid step")
			}
			base = pieces[0]
			n, err := strconv.Atoi(pieces[1])
			if err != nil || n <= 0 {
				return false, fmt.Errorf("invalid step")
			}
			step = n
		}
		start, end, err := cronPartRange(base, lo, hi)
		if err != nil {
			return false, err
		}
		if value >= start && value <= end && (value-start)%step == 0 {
			return true, nil
		}
	}
	return false, nil
}

func truncatePromptPreview(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit])
}

func NewCronCreateTool(scheduler *CronScheduler) *Tools {
	return &Tools{
		Name: "cron_create",
		Func: func(ctx context.Context, args map[string]any) string {
			cronExpr, ok := args["cron"].(string)
			if !ok || strings.TrimSpace(cronExpr) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "cron"))
			}
			taskPrompt, ok := args["prompt"].(string)
			if !ok || strings.TrimSpace(taskPrompt) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "prompt"))
			}
			recurring := true
			if v, ok := args["recurring"].(bool); ok {
				recurring = v
			}
			durable := false
			if v, ok := args["durable"].(bool); ok {
				durable = v
			}
			task, err := scheduler.Create(cronExpr, taskPrompt, recurring, durable)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			mode := "one-shot"
			if task.Recurring {
				mode = "recurring"
			}
			store := "session-only"
			if task.Durable {
				store = "durable"
			}
			return fmt.Sprintf("Created task %s (%s, %s): cron=%s", task.ID, mode, store, task.Cron)
		},
	}
}

func NewCronDeleteTool(scheduler *CronScheduler) *Tools {
	return &Tools{
		Name: "cron_delete",
		Func: func(ctx context.Context, args map[string]any) string {
			taskID, ok := args["id"].(string)
			if !ok || strings.TrimSpace(taskID) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "id"))
			}
			result, err := scheduler.Delete(taskID)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func NewCronListTool(scheduler *CronScheduler) *Tools {
	return &Tools{
		Name: "cron_list",
		Func: func(ctx context.Context, args map[string]any) string {
			return scheduler.ListTasks()
		},
	}
}

func (t *Tools) CronCreateInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "cron_create",
			Description: prompt.CronCreatePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"cron": client.ParameterProperty{
						Type:        "string",
						Description: "5-field cron expression: 'min hour dom month dow'.",
					},
					"prompt": client.ParameterProperty{
						Type:        "string",
						Description: "The prompt to inject into the conversation when the schedule fires.",
					},
					"recurring": client.ParameterProperty{
						Type:        "boolean",
						Description: "true=repeat, false=fire once then delete. Default true.",
					},
					"durable": client.ParameterProperty{
						Type:        "boolean",
						Description: "true=persist to ~/.cc_mini_go project storage, false=session-only. Default false.",
					},
				},
				Required: []string{"cron", "prompt"},
			},
		},
	}
}

func (t *Tools) CronDeleteInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "cron_delete",
			Description: prompt.CronDeletePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"id": client.ParameterProperty{
						Type:        "string",
						Description: "Scheduled task ID to delete.",
					},
				},
				Required: []string{"id"},
			},
		},
	}
}

func (t *Tools) CronListInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "cron_list",
			Description: prompt.CronListPrompt,
			Parameters: client.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
	}
}
