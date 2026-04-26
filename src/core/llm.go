package core

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
)

type openAillm struct {
	model *openai.ChatModel
}

type claudellm struct {
	model *claude.ChatModel
}

func NewOpenAIChatModel(ctx context.Context, apiKey, model, baseURL string, reasoningEffort string) (*openAillm, error) {
	var effort openai.ReasoningEffortLevel
	switch reasoningEffort {
	case "low":
		effort = openai.ReasoningEffortLevelLow
	case "medium":
		effort = openai.ReasoningEffortLevelMedium
	case "high":
		effort = openai.ReasoningEffortLevelHigh
	default:
		return nil, fmt.Errorf("invalid reasoning effort level: %s", reasoningEffort)
	}
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:          apiKey,
		Model:           model,
		BaseURL:         baseURL,
		ReasoningEffort: effort,
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing openai chat model: %v", err)
	}
	return &openAillm{model: chatModel}, nil
}

func NewClaudeChatModel(ctx context.Context, apiKey, model, baseURL string) (*claudellm, error) {
	cm, err := claude.NewChatModel(ctx, &claude.Config{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: &baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing claudellm chat model: %v", err)
	}
	return &claudellm{model: cm}, nil
}
