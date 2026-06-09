package system

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/agent_tools/shared"
	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

func NewTimeNowTool() *Tools {
	timeNowToolUse := func(ctx context.Context, args map[string]interface{}) string {
		region, ok := args["region"].(string)
		if !ok || region == "" {
			return shared.JsonErr(fmt.Sprintf(errors.ErrToolFunctionCall, "region"))
		}
		res, err := timeNow(region)
		if err != nil {
			// 透传真实错误（如 "unknown time zone XXX"），便于 LLM 纠正
			return shared.JsonErr(err.Error())
		}
		b, _ := json.Marshal(map[string]string{"time": res})
		return string(b)
	}
	return &Tools{
		Name: "time_now",
		Func: timeNowToolUse,
	}
}

func timeNow(region string) (string, error) {
	// 1. 加载时区（如 "America/New_York", "UTC", "Asia/Shanghai"）
	loc, err := time.LoadLocation(region)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errors.ErrInvalidTimezone, err)
	}

	// 2. 获取指定时区的当前时间
	now := time.Now().In(loc)
	return now.Format("2006-01-02 15:04:05"), nil
}

func (t *Tools) TimeNowInfoForLLm() client.Tool {
	return client.Tool{
		Type: "function",
		Function: client.FunctionDefinition{
			Name:        "time_now",
			Description: "Get the current time for the user",
			Parameters: client.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"region": client.ParameterProperty{
						Type:        "string",
						Description: "IANA timezone, e.g. America/Los_Angeles, Asia/Shanghai, Europe/London",
					},
				},
				Required: []string{"region"},
			},
		},
	}
}
