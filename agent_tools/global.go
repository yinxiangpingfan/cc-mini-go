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

// asInt 把工具参数里的数字安全转成 int。LLM 参数经 json.Unmarshal 进 map[string]any 后，
// 数字默认是 float64；这里同时兼容 float64 / json.Number / int，避免各工具各写一遍断言。
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

var ToDoList PlanningState = PlanningState{
	Items:             []PlanItem{},
	RoundsSinceUpdate: 0,
	MU:                sync.RWMutex{},
}
