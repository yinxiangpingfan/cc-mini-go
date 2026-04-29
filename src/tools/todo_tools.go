package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// TodoWriteRequest 是 TodoWrite 工具的输入参数。
type TodoWriteRequest struct {
	Todos []TodoWriteItem `json:"todos" jsonschema:"required,description=List of todo items to create"`
}

// TodoWriteItem 是单个待办事项的输入。
type TodoWriteItem struct {
	Subject string `json:"subject" jsonschema:"required,description=Brief imperative title, e.g. 'Add unit tests for auth module'"`
	Status  string `json:"status" jsonschema:"description=Initial status: pending, in_progress, completed; default pending"`
}

// TodoWriteResult 是 TodoWrite 工具的输出。
type TodoWriteResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// TodoUpdateRequest 是 TodoUpdate 工具的输入参数。
type TodoUpdateRequest struct {
	ID      string `json:"id" jsonschema:"required,description=The todo item ID (e.g. '1')"`
	Status  string `json:"status" jsonschema:"description=New status: pending, in_progress, completed"`
	Subject string `json:"subject" jsonschema:"description=New subject text (optional)"`
}

// TodoUpdateResult 是 TodoUpdate 工具的输出。
type TodoUpdateResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// NewTodoWriteTool 创建 TodoWrite 工具，用于创建或替换任务清单。
func NewTodoWriteTool(tm *TodoManager) (tool.InvokableTool, error) {
	return utils.InferTool("TodoWrite",
		"Create or replace the task checklist shown to the user. "+
			"Use when starting a multi-step task to track progress. "+
			"Each item has a subject (brief imperative title) and an optional "+
			"initial status (pending by default).\n\n"+
			"Status values:\n"+
			"- pending: Not started\n"+
			"- in_progress: Currently working on\n"+
			"- completed: Done\n\n"+
			"Example:\n"+
			`  {"todos": [{"subject": "Analyze existing code", "status": "pending"}, {"subject": "Implement new feature"}]}`,
		func(ctx context.Context, req *TodoWriteRequest) (*TodoWriteResult, error) {
			// 清空现有清单
			tm.Clear()

			// 创建新的待办事项
			for _, entry := range req.Todos {
				status := TodoStatus(entry.Status)
				if status == "" {
					status = TodoPending
				}
				tm.Create(entry.Subject, status)
			}

			// 构造返回信息
			items := tm.GetItems()
			result := fmt.Sprintf("Created %d todo items.\n", len(items))
			for _, item := range items {
				result += fmt.Sprintf("  #%s [%s] %s\n", item.ID, item.Status, item.Subject)
			}

			return &TodoWriteResult{Content: result}, nil
		})
}

// NewTodoUpdateTool 创建 TodoUpdate 工具，用于更新单个待办事项。
func NewTodoUpdateTool(tm *TodoManager) (tool.InvokableTool, error) {
	return utils.InferTool("TodoUpdate",
		"Update a todo item's status or subject. "+
			"Set status to in_progress when starting work, completed when done.\n\n"+
			"Status values:\n"+
			"- pending: Not started\n"+
			"- in_progress: Currently working on\n"+
			"- completed: Done\n\n"+
			"Example:\n"+
			`  {"id": "1", "status": "in_progress"}`,
		func(ctx context.Context, req *TodoUpdateRequest) (*TodoUpdateResult, error) {
			status := TodoStatus(req.Status)
			item := tm.Update(req.ID, status, req.Subject)
			if item == nil {
				return &TodoUpdateResult{
					Content: fmt.Sprintf("Todo item #%s not found.", req.ID),
					IsError: true,
				}, nil
			}

			return &TodoUpdateResult{
				Content: fmt.Sprintf("Updated #%s: [%s] %s", item.ID, item.Status, item.Subject),
			}, nil
		})
}
