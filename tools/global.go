package tools

type Tools struct {
	//工具的名称
	Name string
	//用于运行工具
	Func func(input map[string]any) string `json:"-"`
}
