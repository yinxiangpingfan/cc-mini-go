package agent_tools

type Tools struct {
	//工具的名称
	Name string
	//用于运行工具
	Func func(input map[string]any) string `json:"-"`
}

// ReadFiles 记录当前会话中已经读取过的文件路径和哈希值
var ReadFiles = make(map[string]string)
