package agent

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/yinxiangpingfan/cc-mini-go/client"
)

func (a *ChatCompletionAgent) toolsUse(toolsNeed []client.ToolCall) []client.ToolsMessage {
	var wg sync.WaitGroup
	results := make([]client.ToolsMessage, len(toolsNeed))
	for _, v := range toolsNeed {
		switch v.Function.Name {
		case "time_now":
			wg.Go(func() {
				var args map[string]interface{}
				json.Unmarshal([]byte(v.Function.Arguments), &args)
				res, err := a.tools.TimeNowToolUse(args)
				if err != nil {
					results = append(results, *a.call.Cm.NewToolsMessage(v.Id, fmt.Sprintf("{\"error\": \"%s\"}", err.Error())))
					return
				}
				results = append(results, client.ToolsMessage{
					Role:    "tool",
					Content: res,
					ToolsId: v.Id,
				})
			})
		}
	}
	wg.Wait()
	return results
}
