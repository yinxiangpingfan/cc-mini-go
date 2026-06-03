package agent

import (
	tool "github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/prompt"
)

// withSkillCatalog 把轻量的 skill 目录拼到 system prompt 末尾（发现层）。
// 只放名称和描述，完整正文由模型按需通过 load_skill 工具加载。
func (a *ChatCompletionAgent) withSkillCatalog(system string) string {
	catalog := tool.Skills.DescribeAvailable()
	if catalog == "(no skills available)" {
		return system
	}
	return system + "\n\n" + prompt.SkillCatalogHeader + "\n" + catalog
}

// withMemory 把跨会话记忆拼进 system prompt（读取层，对应 skill 的发现层）。
// 与 skill 不同：记忆很小且是「长期方向」，正文全部注入，无需按需加载。无记忆时原样返回。
func (a *ChatCompletionAgent) withMemory(system string) string {
	section := tool.Memory.Describe()
	if section == "" {
		return system
	}
	return system + "\n\n" + prompt.MemoryHeader + "\n" + section
}

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
