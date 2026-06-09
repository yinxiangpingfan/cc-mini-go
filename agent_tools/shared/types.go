package shared

import (
	"context"
	"encoding/json"
)

// ToolFunc 工具执行函数签名：接收 ctx（用于取消长任务）与参数，返回 JSON 字符串结果。
type ToolFunc = func(ctx context.Context, input map[string]any) string

// Tools 工具注册结构体：Name 用于工具调度，Func 用于执行。
type Tools struct {
	Name string
	Func ToolFunc `json:"-"`
}

// JsonErr 把错误信息包装成 {"error":"..."} JSON 字符串。
func JsonErr(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

// AsInt 把工具参数里的数字安全转成 int。LLM 参数经 json.Unmarshal 进 map[string]any 后，
// 数字默认是 float64；这里同时兼容 float64 / json.Number / int，避免各工具各写一遍断言。
func AsInt(v any) (int, bool) {
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
