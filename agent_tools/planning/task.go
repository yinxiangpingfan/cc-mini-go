package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// TaskStatus 任务状态枚举（s12 教学版：4 个状态）
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskDeleted    TaskStatus = "deleted"
)

// validTaskStatuses 用于校验 LLM 传入的 status 值
var validTaskStatuses = map[TaskStatus]bool{
	TaskPending: true, TaskInProgress: true, TaskCompleted: true, TaskDeleted: true,
}

// taskStatusMarkers 把任务状态映射成清单前的标记符号（对齐源码 Python 版 list_all）
var taskStatusMarkers = map[TaskStatus]string{
	TaskPending:    "[ ]",
	TaskInProgress: "[>]",
	TaskCompleted:  "[x]",
	TaskDeleted:    "[-]",
}

// TaskRecord 持久化任务节点：工作目标 + 依赖关系。
// 注意：这是 durable work item，不是 runtime execution slot（后者属 s13）。
type TaskRecord struct {
	ID          int        `json:"id"`
	Subject     string     `json:"subject"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	BlockedBy   []int      `json:"blockedBy"`
	Blocks      []int      `json:"blocks"`
	Owner       string     `json:"owner"`
}

// TaskManager 管理会话级持久化任务图。
// 布局：sessionStorageDir/tasks/task_<id>.json
// 与 MemoryStore 相同的锁模式：工具 goroutine 并发期间读写需要互斥。
type TaskManager struct {
	dir    string
	nextID int
	mu     sync.RWMutex
}

// NewTaskManager 创建 TaskManager，扫描 shared.TaskDir() 初始化 nextID。
func NewTaskManager() *TaskManager {
	dir := shared.TaskDir()
	m := &TaskManager{dir: dir}
	m.nextID = m.maxID() + 1
	return m
}

// maxID 扫描目录获取现有最大 ID，空目录返回 0。
func (m *TaskManager) maxID() int {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimPrefix(e.Name(), "task_")
		name = strings.TrimSuffix(name, ".json")
		if id, err := strconv.Atoi(name); err == nil && id > max {
			max = id
		}
	}
	return max
}

// load 读取单条任务文件。调用方须持锁或保证无并发。
func (m *TaskManager) load(taskID int) (TaskRecord, error) {
	path := filepath.Join(m.dir, fmt.Sprintf("task_%d.json", taskID))
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskRecord{}, fmt.Errorf(errors.ErrTaskNotFound, taskID)
	}
	var task TaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return TaskRecord{}, fmt.Errorf("%w: %v", errors.ErrTaskWrite, err)
	}
	return task, nil
}

// save 写入单条任务文件。调用方须持锁。
func (m *TaskManager) save(task TaskRecord) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", errors.ErrTaskWrite, err)
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %v", errors.ErrTaskWrite, err)
	}
	path := filepath.Join(m.dir, fmt.Sprintf("task_%d.json", task.ID))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("%w: %v", errors.ErrTaskWrite, err)
	}
	return nil
}

// allTasks 扫描目录加载所有任务。调用方须持锁。
func (m *TaskManager) allTasks() []TaskRecord {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil
	}
	tasks := make([]TaskRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue
		}
		var t TaskRecord
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		tasks = append(tasks, t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks
}

// Create 创建新任务并落盘。
func (m *TaskManager) Create(subject, description string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task := TaskRecord{
		ID:          m.nextID,
		Subject:     subject,
		Description: description,
		Status:      TaskPending,
		BlockedBy:   []int{},
		Blocks:      []int{},
		Owner:       "",
	}
	if err := m.save(task); err != nil {
		return "", err
	}
	m.nextID++
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// Get 读取单条任务。
func (m *TaskManager) Get(taskID int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, err := m.load(taskID)
	if err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// Update 更新任务字段：status / owner / addBlockedBy / addBlocks。
// status=completed 时自动调 clearDependency 解锁后续任务。
// addBlocks 时自动维护被阻塞任务的 blockedBy（双向）。
func (m *TaskManager) Update(taskID int, status string, owner string,
	addBlockedBy []int, addBlocks []int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.load(taskID)
	if err != nil {
		return "", err
	}

	if owner != "" {
		task.Owner = owner
	}

	if status != "" {
		ts := TaskStatus(status)
		if !validTaskStatuses[ts] {
			return "", fmt.Errorf(errors.ErrTaskInvalidStatus, status)
		}
		task.Status = ts
		if ts == TaskCompleted {
			m.clearDependency(taskID)
		}
	}

	if len(addBlockedBy) > 0 {
		task.BlockedBy = uniqueAppend(task.BlockedBy, addBlockedBy)
	}

	if len(addBlocks) > 0 {
		task.Blocks = uniqueAppend(task.Blocks, addBlocks)
		// 双向：同步更新被阻塞任务的 blockedBy
		for _, blockedID := range addBlocks {
			blocked, err := m.load(blockedID)
			if err != nil {
				continue
			}
			if !containsInt(blocked.BlockedBy, taskID) {
				blocked.BlockedBy = append(blocked.BlockedBy, taskID)
				_ = m.save(blocked)
			}
		}
	}

	if err := m.save(task); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// clearDependency 完成任务后，从所有其他任务的 blockedBy 中移除 completedID。
func (m *TaskManager) clearDependency(completedID int) {
	for _, t := range m.allTasks() {
		if containsInt(t.BlockedBy, completedID) {
			t.BlockedBy = removeInt(t.BlockedBy, completedID)
			_ = m.save(t)
		}
	}
}

// ListAll 列出所有任务，返回格式化文本（对齐源码 Python 版）。
func (m *TaskManager) ListAll() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := m.allTasks()
	if len(tasks) == 0 {
		return "No tasks."
	}
	lines := make([]string, 0, len(tasks))
	for _, t := range tasks {
		marker, ok := taskStatusMarkers[t.Status]
		if !ok {
			marker = "[?]"
		}
		line := fmt.Sprintf("%s #%d: %s", marker, t.ID, t.Subject)
		if t.Owner != "" {
			line += fmt.Sprintf(" owner=%s", t.Owner)
		}
		if len(t.BlockedBy) > 0 {
			line += fmt.Sprintf(" (blocked by: %v)", t.BlockedBy)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// --- 辅助函数 ---

// uniqueAppend 把 add 中不存在于 base 的元素追加 to base 返回新切片。
func uniqueAppend(base, add []int) []int {
	set := make(map[int]bool, len(base))
	for _, v := range base {
		set[v] = true
	}
	result := make([]int, len(base))
	copy(result, base)
	for _, v := range add {
		if !set[v] {
			result = append(result, v)
			set[v] = true
		}
	}
	return result
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeInt(s []int, v int) []int {
	result := make([]int, 0, len(s))
	for _, x := range s {
		if x != v {
			result = append(result, x)
		}
	}
	return result
}

// --- 4 个工具构造函数 ---

// NewTaskCreateTool 返回 task_create 工具
func NewTaskCreateTool(mgr *TaskManager) *Tools {
	return &Tools{
		Name: "task_create",
		Func: func(ctx context.Context, args map[string]any) string {
			subject, ok := args["subject"].(string)
			if !ok || strings.TrimSpace(subject) == "" {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "subject"))
			}
			description, _ := args["description"].(string)
			result, err := mgr.Create(subject, description)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

// NewTaskUpdateTool 返回 task_update 工具
func NewTaskUpdateTool(mgr *TaskManager) *Tools {
	return &Tools{
		Name: "task_update",
		Func: func(ctx context.Context, args map[string]any) string {
			taskID, ok := shared.AsInt(args["task_id"])
			if !ok {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "task_id"))
			}
			status, _ := args["status"].(string)
			owner, _ := args["owner"].(string)
			addBlockedBy := toIntSlice(args["addBlockedBy"])
			addBlocks := toIntSlice(args["addBlocks"])

			result, err := mgr.Update(taskID, status, owner, addBlockedBy, addBlocks)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

// NewTaskGetTool 返回 task_get 工具
func NewTaskGetTool(mgr *TaskManager) *Tools {
	return &Tools{
		Name: "task_get",
		Func: func(ctx context.Context, args map[string]any) string {
			taskID, ok := shared.AsInt(args["task_id"])
			if !ok {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "task_id"))
			}
			result, err := mgr.Get(taskID)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			return result
		},
	}
}

// NewTaskListTool 返回 task_list 工具
func NewTaskListTool(mgr *TaskManager) *Tools {
	return &Tools{
		Name: "task_list",
		Func: func(ctx context.Context, args map[string]any) string {
			return mgr.ListAll()
		},
	}
}

// toIntSlice 把 LLM 传入的 []interface{} (内含 float64) 转为 []int。
func toIntSlice(v any) []int {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]int, 0, len(arr))
	for _, item := range arr {
		if n, ok := shared.AsInt(item); ok {
			result = append(result, n)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// --- 4 个工具的 LLM schema ---

func (t *Tools) TaskCreateInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "task_create",
			Description: prompt.TaskCreatePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"subject": client.ParameterProperty{
						Type:        "string",
						Description: "Brief imperative title, e.g. 'Write parser module'.",
					},
					"description": client.ParameterProperty{
						Type:        "string",
						Description: "Optional longer description of the task.",
					},
				},
				Required: []string{"subject"},
			},
		},
	}
}

func (t *Tools) TaskUpdateInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "task_update",
			Description: prompt.TaskUpdatePrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"task_id": client.ParameterProperty{
						Type:        "integer",
						Description: "The ID of the task to update.",
					},
					"status": client.ParameterProperty{
						Type:        "string",
						Description: "New status for the task.",
						Enum:        []string{"pending", "in_progress", "completed", "deleted"},
					},
					"owner": client.ParameterProperty{
						Type:        "string",
						Description: "Set when a teammate claims the task.",
					},
					"addBlockedBy": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer"},
						"description": "Task IDs that must complete before this task can start.",
					},
					"addBlocks": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "integer"},
						"description": "Task IDs that this task will unblock when completed.",
					},
				},
				Required: []string{"task_id"},
			},
		},
	}
}

func (t *Tools) TaskGetInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "task_get",
			Description: prompt.TaskGetPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"task_id": client.ParameterProperty{
						Type:        "integer",
						Description: "The ID of the task to retrieve.",
					},
				},
				Required: []string{"task_id"},
			},
		},
	}
}

func (t *Tools) TaskListInfoForLLM() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "task_list",
			Description: prompt.TaskListPrompt,
			Parameters: client.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
	}
}
