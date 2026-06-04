package agent

import (
	tool "github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// 注：system prompt 的组装（含 skill 目录、memory）已迁到 system_prompt.go 的 buildSystemPrompt 流水线。

func (a *ChatCompletionAgent) ToolInit(tools *map[string]tool.ToolFunc) []client.Tool {
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
	loadSkillTool := tool.NewLoadSkillTool(tool.Skills)
	(*tools)[loadSkillTool.Name] = loadSkillTool.Func
	compactTool := tool.NewCompactTool()
	(*tools)[compactTool.Name] = compactTool.Func
	editFileTool := tool.NewEditFileTool()
	(*tools)[editFileTool.Name] = editFileTool.Func
	grepTool := tool.NewGrepTool()
	(*tools)[grepTool.Name] = grepTool.Func
	globTool := tool.NewGlobTool()
	(*tools)[globTool.Name] = globTool.Func
	saveMemoryTool := tool.NewSaveMemoryTool(tool.Memory)
	(*tools)[saveMemoryTool.Name] = saveMemoryTool.Func
	deleteMemoryTool := tool.NewDeleteMemoryTool(tool.Memory)
	(*tools)[deleteMemoryTool.Name] = deleteMemoryTool.Func
	return []client.Tool{
		timeNowTool.TimeNowInfoForLLm(),
		readFileTool.ReadFileInfoForLLm(),
		writeFileTool.WriteFileInfoForLLm(),
		baseTool.BashToolForLLM(),
		todoListTool.TodoListInfoLLm(),
		subAgentTool.SubAgentInfoForLLM(),
		loadSkillTool.LoadSkillInfoForLLM(),
		compactTool.CompactInfoForLLM(),
		editFileTool.EditFileInfoForLLm(),
		grepTool.GrepInfoForLLm(),
		globTool.GlobInfoForLLm(),
		saveMemoryTool.SaveMemoryInfoForLLM(),
		deleteMemoryTool.DeleteMemoryInfoForLLM(),
	}
}
