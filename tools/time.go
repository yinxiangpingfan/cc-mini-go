package tools

import (
	"fmt"
	"time"

	"github.com/yinxiangpingfan/cc-mini-go/client"
	"github.com/yinxiangpingfan/cc-mini-go/errors"
)

func NewTimeNowTool() *Tools {
	timeNowToolUse := func(args map[string]interface{}) string {
		region, exists := args["region"]
		if !exists {
			return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Sprintf(errors.ErrToolFunctionCall, "region"))
		}
		res, err := timeNow(region.(string))
		if err != nil {
			return fmt.Sprintf("{\"error\": \"%s\"}", fmt.Sprintf(errors.ErrToolFunctionCall, "region"))
		}
		return fmt.Sprintf("{\"time\": \"%s\"}", res)
	}
	return &Tools{
		Name: "time_now",
		Func: timeNowToolUse,
	}
}

func timeNow(region string) (string, error) {
	// 1. 加载时区
	loc, err := time.LoadLocation(region) // 或 "America/New_York", "UTC"
	if err != nil {
		fmt.Println("时区加载失败:", err)
		return "", err
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
				Properties: map[string]client.ParameterProperty{
					//地区
					"region": {
						Type:        "string",
						Description: "IANA timezone, e.g. America/Los_Angeles, Asia/Shanghai, Europe/London",
					},
				},
				Required: []string{"region"},
			},
		},
	}
}
