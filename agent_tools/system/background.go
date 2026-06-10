package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

const (
	defaultBackgroundTimeout = 300 * time.Second
	backgroundPreviewChars   = 500
	backgroundMaxOutputChars = 50000
)

type BackgroundStatus string

const (
	BackgroundRunning   BackgroundStatus = "running"
	BackgroundCompleted BackgroundStatus = "completed"
	BackgroundTimeout   BackgroundStatus = "timeout"
	BackgroundError     BackgroundStatus = "error"
)

type RuntimeTaskRecord struct {
	ID            string           `json:"id"`
	Status        BackgroundStatus `json:"status"`
	Result        string           `json:"result,omitempty"`
	Command       string           `json:"command"`
	StartedAt     float64          `json:"started_at"`
	FinishedAt    *float64         `json:"finished_at"`
	ResultPreview string           `json:"result_preview"`
	OutputFile    string           `json:"output_file"`
}

type BackgroundNotification struct {
	TaskID     string           `json:"task_id"`
	Status     BackgroundStatus `json:"status"`
	Command    string           `json:"command"`
	Preview    string           `json:"preview"`
	OutputFile string           `json:"output_file"`
}

type BackgroundManager struct {
	dir           string
	tasks         map[string]*RuntimeTaskRecord
	notifications []BackgroundNotification
	mu            sync.RWMutex
}

func NewBackgroundManager() *BackgroundManager {
	return &BackgroundManager{
		dir:   shared.RuntimeTasksDir(),
		tasks: make(map[string]*RuntimeTaskRecord),
	}
}

func (m *BackgroundManager) recordPath(taskID string) string {
	return filepath.Join(m.dir, taskID+".json")
}

func (m *BackgroundManager) outputPath(taskID string) string {
	return filepath.Join(m.dir, taskID+".log")
}

func newBackgroundTaskID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return fmt.Sprintf("%x", b[:])
	}
	return fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
}

func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(time.Second)
}

func compactPreview(output string, limit int) string {
	if output == "" {
		output = "(no output)"
	}
	compact := strings.Join(strings.Fields(output), " ")
	if compact == "" {
		compact = "(no output)"
	}
	if utf8.RuneCountInString(compact) > limit {
		runes := []rune(compact)
		compact = string(runes[:limit])
	}
	return compact
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit])
}

func (m *BackgroundManager) persistTaskLocked(taskID string) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("%w: %w", errors.ErrBackgroundTaskWrite, err)
	}
	task, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf(errors.ErrBackgroundTaskNotFound, taskID)
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %w", errors.ErrBackgroundTaskWrite, err)
	}
	if err := os.WriteFile(m.recordPath(taskID), data, 0o644); err != nil {
		return fmt.Errorf("%w: %w", errors.ErrBackgroundTaskWrite, err)
	}
	return nil
}

func (m *BackgroundManager) Run(command string, timeout time.Duration) (RuntimeTaskRecord, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return RuntimeTaskRecord{}, fmt.Errorf(errors.ErrToolFunctionCall, "command")
	}
	if timeout <= 0 {
		timeout = defaultBackgroundTimeout
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return RuntimeTaskRecord{}, fmt.Errorf("%w: %w", errors.ErrBackgroundTaskWrite, err)
	}

	taskID := newBackgroundTaskID()
	outputFile := m.outputPath(taskID)
	task := &RuntimeTaskRecord{
		ID:            taskID,
		Status:        BackgroundRunning,
		Command:       command,
		StartedAt:     unixSeconds(time.Now()),
		ResultPreview: "",
		OutputFile:    outputFile,
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	err := m.persistTaskLocked(taskID)
	m.mu.Unlock()
	if err != nil {
		return RuntimeTaskRecord{}, err
	}

	go m.execute(taskID, command, timeout)
	return *task, nil
}

func (m *BackgroundManager) execute(taskID, command string, timeout time.Duration) {
	cmdCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(cmdCtx, shell, "-c", command)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()

	status := BackgroundCompleted
	var output string
	if stdoutBuf.Len() > 0 {
		output = strings.TrimRight(stdoutBuf.String(), "\n\r")
	}
	if stderrBuf.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "[stderr]\n" + strings.TrimRight(stderrBuf.String(), "\n\r ")
	}
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			status = BackgroundTimeout
			output = fmt.Sprintf("Error: Timeout (%s)", timeout)
		} else if exitError, ok := err.(*exec.ExitError); ok {
			if output != "" {
				output += "\n"
			}
			output += fmt.Sprintf("[exit code: %d]", exitError.ExitCode())
		} else {
			status = BackgroundError
			if output != "" {
				output += "\n"
			}
			output += fmt.Sprintf("%s: %v", errors.ErrBackgroundTaskExec, err)
		}
	}
	if output == "" {
		output = "(no output)"
	}
	output = truncateRunes(output, backgroundMaxOutputChars)
	preview := compactPreview(output, backgroundPreviewChars)
	finished := unixSeconds(time.Now())

	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[taskID]; ok {
		task.Status = status
		task.Result = output
		task.FinishedAt = &finished
		task.ResultPreview = preview
		_ = os.WriteFile(m.outputPath(taskID), []byte(output), 0o644)
		_ = m.persistTaskLocked(taskID)
		m.notifications = append(m.notifications, BackgroundNotification{
			TaskID:     taskID,
			Status:     status,
			Command:    truncateRunes(command, 80),
			Preview:    preview,
			OutputFile: task.OutputFile,
		})
	}
}

func (m *BackgroundManager) Check(taskID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if taskID != "" {
		task, ok := m.tasks[taskID]
		if !ok {
			return "", fmt.Errorf(errors.ErrBackgroundTaskNotFound, taskID)
		}
		visible := map[string]any{
			"id":             task.ID,
			"status":         task.Status,
			"command":        task.Command,
			"result_preview": task.ResultPreview,
			"output_file":    task.OutputFile,
		}
		data, _ := json.MarshalIndent(visible, "", "  ")
		return string(data), nil
	}

	tasks := make([]*RuntimeTaskRecord, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].StartedAt < tasks[j].StartedAt })
	if len(tasks) == 0 {
		return "No background tasks.", nil
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		preview := task.ResultPreview
		if preview == "" {
			preview = "(running)"
		}
		lines = append(lines, fmt.Sprintf("%s: [%s] %s -> %s", task.ID, task.Status, truncateRunes(task.Command, 60), preview))
	}
	return strings.Join(lines, "\n"), nil
}

func (m *BackgroundManager) DrainNotifications() []BackgroundNotification {
	m.mu.Lock()
	defer m.mu.Unlock()
	notifs := append([]BackgroundNotification(nil), m.notifications...)
	m.notifications = nil
	return notifs
}

func FormatBackgroundNotifications(notifs []BackgroundNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(notifs)+2)
	lines = append(lines, "<background-results>")
	for _, n := range notifs {
		lines = append(lines, fmt.Sprintf("[bg:%s] %s: %s (output_file=%s)", n.TaskID, n.Status, n.Preview, n.OutputFile))
	}
	lines = append(lines, "</background-results>")
	return strings.Join(lines, "\n")
}

func NewBackgroundRunTool(mgr *BackgroundManager) *Tools {
	return &Tools{
		Name: "background_run",
		Func: func(ctx context.Context, args map[string]any) string {
			command, ok := args["command"].(string)
			if !ok || strings.TrimSpace(command) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "command"))
			}
			timeout := defaultBackgroundTimeout
			if n, ok := shared.AsInt(args["timeout"]); ok && n > 0 {
				timeout = time.Duration(n) * time.Second
			}
			task, err := mgr.Run(command, timeout)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			data, _ := json.Marshal(map[string]any{
				"task_id":     task.ID,
				"status":      task.Status,
				"command":     task.Command,
				"output_file": task.OutputFile,
			})
			return string(data)
		},
	}
}

func NewCheckBackgroundTool(mgr *BackgroundManager) *Tools {
	return &Tools{
		Name: "check_background",
		Func: func(ctx context.Context, args map[string]any) string {
			taskID, _ := args["task_id"].(string)
			result, err := mgr.Check(taskID)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

func (t *Tools) BackgroundRunInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "background_run",
			Description: prompt.BackgroundRunPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"command": client.ParameterProperty{
						Type:        "string",
						Description: "The shell command to run in the background.",
					},
					"description": client.ParameterProperty{
						Type:        "string",
						Description: "Clear, concise description of what this command does.",
					},
					"timeout": client.ParameterProperty{
						Type:        "integer",
						Description: "Timeout in seconds (default 300).",
					},
				},
				Required: []string{"command"},
			},
		},
	}
}

func (t *Tools) CheckBackgroundInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "check_background",
			Description: prompt.CheckBackgroundPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"task_id": client.ParameterProperty{
						Type:        "string",
						Description: "Optional background task ID. Omit to list all background tasks.",
					},
				},
			},
		},
	}
}
