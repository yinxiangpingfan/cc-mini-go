package agent

import (
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/ctxmgmt"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/fileops"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/planning"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/system"
	"github.com/yinxiangpingfan/cc-mini-go/client"
)

// 注：system prompt 的组装（含 skill 目录、memory）已迁到 system_prompt.go 的 buildSystemPrompt 流水线。

func (a *ChatCompletionAgent) ToolInit(tools *map[string]shared.ToolFunc) []client.Tool {
	timeNowTool := system.NewTimeNowTool()
	(*tools)[timeNowTool.Name] = timeNowTool.Func
	readFileTool := fileops.NewReadFile()
	(*tools)[readFileTool.Name] = readFileTool.Func
	writeFileTool := fileops.NewWriteFileTool()
	(*tools)[writeFileTool.Name] = writeFileTool.Func
	baseTool := system.NewBashTool()
	(*tools)[baseTool.Name] = baseTool.Func
	todoListTool := planning.NewTodoListTool()
	(*tools)[todoListTool.Name] = todoListTool.Func
	subAgentTool := system.NewSubAgentTools(a.call, a.cf.Model)
	(*tools)[subAgentTool.Name] = subAgentTool.Func
	loadSkillTool := ctxmgmt.NewLoadSkillTool(ctxmgmt.Skills)
	(*tools)[loadSkillTool.Name] = loadSkillTool.Func
	compactTool := ctxmgmt.NewCompactTool()
	(*tools)[compactTool.Name] = compactTool.Func
	editFileTool := fileops.NewEditFileTool()
	(*tools)[editFileTool.Name] = editFileTool.Func
	grepTool := fileops.NewGrepTool()
	(*tools)[grepTool.Name] = grepTool.Func
	globTool := fileops.NewGlobTool()
	(*tools)[globTool.Name] = globTool.Func
	saveMemoryTool := ctxmgmt.NewSaveMemoryTool(ctxmgmt.Memory)
	(*tools)[saveMemoryTool.Name] = saveMemoryTool.Func
	deleteMemoryTool := ctxmgmt.NewDeleteMemoryTool(ctxmgmt.Memory)
	(*tools)[deleteMemoryTool.Name] = deleteMemoryTool.Func
	taskMgr := planning.NewTaskManager()
	taskCreateTool := planning.NewTaskCreateTool(taskMgr)
	(*tools)[taskCreateTool.Name] = taskCreateTool.Func
	taskUpdateTool := planning.NewTaskUpdateTool(taskMgr)
	(*tools)[taskUpdateTool.Name] = taskUpdateTool.Func
	taskGetTool := planning.NewTaskGetTool(taskMgr)
	(*tools)[taskGetTool.Name] = taskGetTool.Func
	taskListTool := planning.NewTaskListTool(taskMgr)
	(*tools)[taskListTool.Name] = taskListTool.Func
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
		taskCreateTool.TaskCreateInfoForLLM(),
		taskUpdateTool.TaskUpdateInfoForLLM(),
		taskGetTool.TaskGetInfoForLLM(),
		taskListTool.TaskListInfoForLLM(),
	}
}
