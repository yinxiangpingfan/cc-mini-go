package agent_tools

import (
	"context"
	"encoding/json"
	"sync"
)

// ToolFunc 工具执行函数签名：接收 ctx（用于取消长任务）与参数，返回 JSON 字符串结果。
// 用类型别名以便与字面量类型互相赋值。
type ToolFunc = func(ctx context.Context, input map[string]any) string

type Tools struct {
	//工具的名称
	Name string
	//用于运行工具
	Func ToolFunc `json:"-"`
}

// ReadFiles 记录当前会话中已经读取过的文件路径和哈希值
var ReadFiles = ReadedFile{
	ReadFiles: make(map[string]string),
	MU:        sync.RWMutex{},
}

func jsonErr(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

var ToDoList PlanningState = PlanningState{
	Items:             []PlanItem{},
	RoundsSinceUpdate: 0,
	MU:                sync.RWMutex{},
}
