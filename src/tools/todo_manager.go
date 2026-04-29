package tools

import (
	"fmt"
	"strconv"
	"sync"
)

// TodoStatus 表示待办事项的状态。
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

// TodoItem 表示一个待办事项。
type TodoItem struct {
	ID      string     `json:"id"`
	Subject string     `json:"subject"`
	Status  TodoStatus `json:"status"`
}

// TodoManager 管理待办事项列表。
type TodoManager struct {
	mu     sync.RWMutex
	items  []*TodoItem
	nextID int
}

// NewTodoManager 创建一个新的 TodoManager。
func NewTodoManager() *TodoManager {
	return &TodoManager{
		items:  make([]*TodoItem, 0),
		nextID: 1,
	}
}

// Create 创建一个新的待办事项，返回创建的事项。
func (tm *TodoManager) Create(subject string, status TodoStatus) *TodoItem {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if status == "" {
		status = TodoPending
	}

	item := &TodoItem{
		ID:      strconv.Itoa(tm.nextID),
		Subject: subject,
		Status:  status,
	}
	tm.nextID++
	tm.items = append(tm.items, item)
	return item
}

// Update 更新待办事项的状态或标题，返回更新后的事项，未找到返回 nil。
func (tm *TodoManager) Update(id string, status TodoStatus, subject string) *TodoItem {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, item := range tm.items {
		if item.ID == id {
			if status != "" {
				item.Status = status
			}
			if subject != "" {
				item.Subject = subject
			}
			return item
		}
	}
	return nil
}

// Get 根据 ID 获取待办事项，未找到返回 nil。
func (tm *TodoManager) Get(id string) *TodoItem {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, item := range tm.items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

// GetItems 返回所有待办事项的副本。
func (tm *TodoManager) GetItems() []*TodoItem {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]*TodoItem, len(tm.items))
	copy(result, tm.items)
	return result
}

// Clear 清空所有待办事项。
func (tm *TodoManager) Clear() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.items = make([]*TodoItem, 0)
	tm.nextID = 1
}

// String 返回待办事项的格式化字符串。
func (tm *TodoManager) String() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.items) == 0 {
		return "No todo items."
	}

	result := ""
	for _, item := range tm.items {
		result += fmt.Sprintf("  #%s [%s] %s\n", item.ID, item.Status, item.Subject)
	}
	return result
}
