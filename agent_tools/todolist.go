package agent_tools

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

type TodoItem struct {
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

const (
	StatusPending    string = "pending"
	StatusInProgress string = "in_progress"
	StatusCompleted  string = "completed"
)

type PlanItem struct {
	todoTtem   TodoItem
	ActiveForm string `json:"activeForm"` // 正在进行时的描述
}

type PlanningState struct {
	Items             []PlanItem
	RoundsSinceUpdate int
	MU                sync.RWMutex
}

func updateTodoList(items []TodoItem) error {
	inProgressCount := 0
	for _, item := range items {
		if item.Status == StatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return fmt.Errorf("only one item can be in_progress at a time")
	}

	planItems := make([]PlanItem, 0, len(items))
	for _, item := range items {
		planItems = append(planItems, PlanItem{
			todoTtem: item,
		})
	}

	ToDoList.MU.Lock()
	ToDoList.Items = planItems
	ToDoList.RoundsSinceUpdate = 0
	ToDoList.MU.Unlock()
	return nil
}

func NewTodoListTool() *Tools {
	return &Tools{
		Name: "todo_list",
		Func: func(args map[string]interface{}) string {
			raw, exists := args["todos"]
			if !exists {
				return jsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "todos"))
			}
			data, err := json.Marshal(raw)
			if err != nil {
				return jsonErr(err.Error())
			}
			var items []TodoItem
			if err := json.Unmarshal(data, &items); err != nil {
				return jsonErr(err.Error())
			}
			if err := updateTodoList(items); err != nil {
				return jsonErr(err.Error())
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
