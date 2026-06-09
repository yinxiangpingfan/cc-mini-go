package planning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

func updateTodoList(items []shared.TodoItem) error {
	inProgressCount := 0
	for _, item := range items {
		if item.Status == shared.StatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return errors.ErrTodoInProgress
	}

	planItems := make([]shared.PlanItem, 0, len(items))
	for _, item := range items {
		planItems = append(planItems, shared.PlanItem{
			TodoItem: item,
		})
	}

	shared.ToDoList.MU.Lock()
	shared.ToDoList.Items = planItems
	shared.ToDoList.RoundsSinceUpdate = 0
	shared.ToDoList.MU.Unlock()
	return nil
}

// Tools defines the tool structure locally to allow package-local method definitions.
type Tools struct {
	Name string
	Func shared.ToolFunc `json:"-"`
}

func NewTodoListTool() *Tools {
	return &Tools{
		Name: "todo_list",
		Func: func(ctx context.Context, args map[string]interface{}) string {
			raw, exists := args["todos"]
			if !exists {
				return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "todos"))
			}
			data, err := json.Marshal(raw)
			if err != nil {
				return shared.JsonErr(err.Error())
			}
			var items []shared.TodoItem
			if err := json.Unmarshal(data, &items); err != nil {
				return shared.JsonErr(err.Error())
			}
			if err := updateTodoList(items); err != nil {
				return shared.JsonErr(err.Error())
			}
			b, _ := json.Marshal(map[string]any{"todos": items})
			return string(b)
		},
	}
}

func (t *Tools) TodoListInfoLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "todo_list",
			Description: prompt.ToDoListPrompt,
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"todos": map[string]any{
						"type":        "array",
						"description": "List of todo items to create.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"subject": map[string]any{
									"type":        "string",
									"description": "Brief imperative title, e.g. 'Add unit tests for auth module'.",
								},
								"status": map[string]any{
									"type":        "string",
									"enum":        []string{"pending", "in_progress", "completed"},
									"description": "Initial status (default: pending).",
								},
							},
							"required": []string{"subject"},
						},
					},
				},
				Required: []string{"todos"},
			},
		},
	}
}
