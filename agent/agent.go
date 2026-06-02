package agent

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/config"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

type ChatCompletionAgent struct {
	cf   *config.Config
	call *client.Call
}

func NewChatCompletionAgent(cf *config.Config, call *client.Call) *ChatCompletionAgent {
	return &ChatCompletionAgent{
		cf:   cf,
		call: call,
	}
}

// 非流式对话请求
func (a *ChatCompletionAgent) Agent(messages []client.Message, system string) ([]any, error) {
	//定义信息
	allMsg := make([]any, 0, len(messages))
	for _, m := range messages {
		allMsg = append(allMsg, m)
	}
	//存储工具信息与调用函数
	tools := make(map[string]func(input map[string]any) string)
	clientTool := a.ToolInit(&tools)
	//把 skill 目录拼进 system prompt（轻量发现层）
	system = a.withSkillCatalog(system)
	//上下文压缩状态（跨轮）
	compactState := agent_tools.NewCompactState()
	//会话转录：整段对话持续以 jsonl 落盘，与压缩解耦
	transcript := agent_tools.NewSessionTranscript()
	//任意返回路径都把最后的消息补写入转录
	defer func() { _ = transcript.Flush(allMsg) }()

	//开始请求LLM
	for {
		// 持续把本轮之前新增的消息落盘（压缩前先写，保住完整历史）
		_ = transcript.Flush(allMsg)
		// 上下文压缩：先微压缩旧工具结果，再判断整体是否过大需要完整压缩
		allMsg = agent_tools.MicroCompact(allMsg)
		if agent_tools.EstimateContextSize(allMsg) > agent_tools.ContextLimit {
			allMsg = agent_tools.CompactHistory(a.call, a.cf.Model, allMsg, compactState, "")
		}

		// 本轮计数 +1（计划已多少轮未更新）
		agent_tools.ToDoList.MU.Lock()
		if len(agent_tools.ToDoList.Items) > 0 {
			agent_tools.ToDoList.RoundsSinceUpdate++
		}
		agent_tools.ToDoList.MU.Unlock()

		// 检查是否需要刷新计划
		agent_tools.ToDoList.MU.RLock()
		needReminder := agent_tools.ToDoList.RoundsSinceUpdate >= 3
		agent_tools.ToDoList.MU.RUnlock()
		if needReminder {
			reminder := a.call.Cm.NewUserMessage("<reminder>Refresh your plan before continuing.</reminder>")
			allMsg = append(allMsg, reminder)
		}

		res, resp, err := a.call.NewCallRequest(a.cf.Model, allMsg, false, system, clientTool, nil)
		if err != nil {
			//TODO:处理错误
			return allMsg, err
		}
		if resp.StatusCode != 200 {
			//TODO:处理错误
			return allMsg, fmt.Errorf(errors.ErrHTTPStatusCode, resp.StatusCode)
		}
		if len(res.Choices) == 0 {
			return allMsg, nil
		}
		//处理LLM返回的信息
		if res.Choices[0].Message.Refusal != "" {
			//大模型拒绝回答
			allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(res.Choices[0].Message.Refusal))
			return allMsg, nil
		}
		//处理工具调用
		if len(res.Choices[0].Message.ToolCalls) > 0 {
			//追加工具请求信息
			allMsg = append(allMsg, *a.call.Cm.NewToolsCall(res.Choices[0].Message.Content, res.Choices[0].Message.ToolCalls))
			//检测本轮是否请求手动压缩
			manualCompact, compactFocus := agent_tools.DetectManualCompact(res.Choices[0].Message.ToolCalls)
			var wg sync.WaitGroup
			var mu sync.Mutex
			for _, v := range res.Choices[0].Message.ToolCalls {
				if f, exists := tools[v.Function.Name]; exists {
					wg.Add(1)
					go func() {
						defer wg.Done()
						var args map[string]any
						json.Unmarshal([]byte(v.Function.Arguments), &args)
						//大结果落盘，只在上下文留预览
						res := agent_tools.PersistLargeOutput(v.Id, f(args))
						mu.Lock()
						//追加工具返回信息
						allMsg = append(allMsg, *a.call.Cm.NewToolsMessage(v.Id, res))
						mu.Unlock()
					}()
				}
			}
			wg.Wait()
			//手动压缩与自动压缩复用同一条机制（压缩前先把本轮消息落盘）
			if manualCompact {
				_ = transcript.Flush(allMsg)
				allMsg = agent_tools.CompactHistory(a.call, a.cf.Model, allMsg, compactState, compactFocus)
			}
		} else {
			//没有工具调用，返回结果
			if s, ok := res.Choices[0].Message.Content.(string); ok && s != "" {
				allMsg = append(allMsg, *a.call.Cm.NewAssistantMessage(s))
			}
			return allMsg, nil
		}
	}
}
