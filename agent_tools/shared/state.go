package shared

import "sync"

// ReadedFile 记录当前会话中已读取过的文件路径和哈希值，用于 edit/write 的脏写检测。
type ReadedFile struct {
	ReadFiles map[string]string
	MU        sync.RWMutex
}

// ReadFileState 全局已读文件状态，由 fileops 工具读写，compact 读取。
var ReadFileState = ReadedFile{
	ReadFiles: make(map[string]string),
	MU:        sync.RWMutex{},
}

// TodoItem 是 todo_list 工具传入的单条待办。
type TodoItem struct {
	Subject string `json:"subject"`
	Status  string `json:"status"`
}

const (
	StatusPending    string = "pending"
	StatusInProgress string = "in_progress"
	StatusCompleted  string = "completed"
)

// PlanItem 是内部包装：TodoItem + 正在进行时描述。
type PlanItem struct {
	TodoItem   TodoItem
	ActiveForm string `json:"activeForm"`
}

// PlanningState 是 todo_list 的全局状态，由 agent 主循环读取展示。
type PlanningState struct {
	Items             []PlanItem
	RoundsSinceUpdate int
	MU                sync.RWMutex
}

// ToDoList 是包级全局 todo 状态。
var ToDoList = PlanningState{
	Items:             []PlanItem{},
	RoundsSinceUpdate: 0,
	MU:                sync.RWMutex{},
}
