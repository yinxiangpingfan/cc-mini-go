package agent

import (
	tool "github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func (a *ChatCompletionAgent) ToolInit(tools *map[string]func(input map[string]any) string) []client.Tool {
	timeNowTool := tool.NewTimeNowTool()
	(*tools)[timeNowTool.Name] = timeNowTool.Func
	readFileTool := tool.NewReadFile()
	(*tools)[readFileTool.Name] = readFileTool.Func
	writeFileTool := tool.NewWriteFileTool()
	(*tools)[writeFileTool.Name] = writeFileTool.Func
	baseTool := tool.NewBashTool()
	(*tools)[baseTool.Name] = baseTool.Func
	todoListTool := tool.NewTodoListTool()
	(*tools)[todoListTool.Name] = todoListTool.Func
	subAgentTool := tool.NewSubAgentTools(a.call, a.cf.Model)
	(*tools)[subAgentTool.Name] = subAgentTool.Func
	return []client.Tool{
		timeNowTool.TimeNowInfoForLLm(),
		readFileTool.ReadFileInfoForLLm(),
		writeFileTool.WriteFileInfoForLLm(),
		baseTool.BashToolForLLM(),
		todoListTool.TodoListInfoLLm(),
		subAgentTool.SubAgentInfoForLLM(),
	}
}
