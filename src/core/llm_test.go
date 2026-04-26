package core

import (
	"cc_mini/src/config"
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNewOpenAIChatModel(t *testing.T) {
	config, _ := config.NewConfig("../config/config.json")
	ctx := context.Background()
	model, err := NewOpenAIChatModel(ctx, config.APIKey, config.Model, config.BaseUrl, config.ReasoningEffort)
	if err != nil {
		t.Errorf("Error creating OpenAI chat model: %v", err)
	}
	outPut, err := model.model.Generate(ctx, []*schema.Message{
		&schema.Message{
			Role:    "user",
			Content: "Hello, how are you?",
		},
	})
	if err != nil {
		t.Errorf("Error generating response: %v", err)
	}
	t.Logf("Response: %s", outPut)
}

func TestNewClaudeChatModel(t *testing.T) {
	config, _ := config.NewConfig("../config/config_claude.json")
	ctx := context.Background()
	model, err := NewClaudeChatModel(ctx, config.APIKey, config.Model, config.BaseUrl)
	if err != nil {
		t.Errorf("Error creating Claude chat model: %v", err)
	}
	outPut, err := model.model.Generate(ctx, []*schema.Message{
		&schema.Message{
			Role:    "user",
			Content: "Hello, how are you?",
		},
	})
	if err != nil {
		t.Errorf("Error generating response: %v", err)
	}
	t.Logf("Response: %s", outPut)
}
