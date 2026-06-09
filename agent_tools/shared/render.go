package shared

import (
	"fmt"
	"strings"
)

// statusMarkers 把任务状态映射成清单前的标记符号。
var statusMarkers = map[string]string{
	StatusPending:    "[ ]",
	StatusInProgress: "[>]",
	StatusCompleted:  "[x]",
}

// Render 把当前计划渲染成多行文本，每行一个任务，前缀为状态标记。
func (p *PlanningState) Render() string {
	p.MU.RLock()
	defer p.MU.RUnlock()

	if len(p.Items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		marker, ok := statusMarkers[item.TodoItem.Status]
		if !ok {
			marker = "[ ]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, item.TodoItem.Subject))
	}
	return strings.Join(lines, "\n")
}
