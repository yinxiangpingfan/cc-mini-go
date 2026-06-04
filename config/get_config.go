package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// 获取配置信息

type Config struct {
	ApiUrl string `json:"base_url"`
	ApiKey string `json:"api_key"`
	Model  string `json:"model"`
	// MaxContextTokens 模型上下文窗口（token）。自动压缩阈值由它推导（见 agent_tools.AutoCompactThreshold）：
	// 窗口 − 预留摘要输出 − 安全余量。0/缺省时用 DefaultMaxContextTokens。
	MaxContextTokens int `json:"max_context_tokens"`
	// Permission 可选：工具权限闸配置；缺省时由消费方（如 TUI）决定默认模式与内置规则。
	Permission PermissionConfig `json:"permission"`
}

// DefaultMaxContextTokens 是未配置 max_context_tokens 时的默认模型上下文窗口（对齐主流 200K 模型）。
const DefaultMaxContextTokens = 200000

// ContextWindow 返回生效的模型上下文窗口：配了用配的，否则用默认。
func (c Config) ContextWindow() int {
	if c.MaxContextTokens > 0 {
		return c.MaxContextTokens
	}
	return DefaultMaxContextTokens
}

// PermissionRuleConfig 是 setting.json 里的一条权限规则（与 agent/core.PermissionRule 同构，
// 但 config 包不依赖 core，由消费方做转换）。
type PermissionRuleConfig struct {
	Tool    string `json:"tool"`
	Content string `json:"content"`
}

// PermissionConfig 是 setting.json 里的 permission 段：模式 + deny/allow 规则。
type PermissionConfig struct {
	Mode  string                 `json:"mode"` // "default" | "plan" | "auto"，空则用消费方默认
	Deny  []PermissionRuleConfig `json:"deny"`
	Allow []PermissionRuleConfig `json:"allow"`
}

func GetConfig() (Config, error) {
	var config Config
	home, err := os.UserHomeDir()
	configPath := filepath.Join(home, ".cc_mini_go", "setting.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, err
	}
	err = json.Unmarshal(data, &config)
	if err != nil {
		return Config{}, err
	}
	return config, nil
}
