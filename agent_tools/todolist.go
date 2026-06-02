package agent_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
		return errors.ErrTodoInProgress
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

// statusMarkers 把任务状态映射成清单前的标记符号。
var statusMarkers = map[string]string{
	StatusPending:    "[ ]",
	StatusInProgress: "[>]",
	StatusCompleted:  "[x]",
}

// Render 把当前计划渲染成多行文本，每行一个任务，前缀为状态标记。
// 等价于示例中的 Python render：未知状态兜底为 "[ ]"，空计划返回空串。
// 内部加读锁，可在 agent 循环中安全并发调用。
func (p *PlanningState) Render() string {
	p.MU.RLock()
	defer p.MU.RUnlock()

	if len(p.Items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		marker, ok := statusMarkers[item.todoTtem.Status]
		if !ok {
			marker = "[ ]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, item.todoTtem.Subject))
	}
	return strings.Join(lines, "\n")
}

func NewTodoListTool() *Tools {
	return &Tools{
		Name: "todo_list",
		Func: func(ctx context.Context, args map[string]interface{}) string {
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
