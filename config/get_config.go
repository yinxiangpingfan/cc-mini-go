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
	// Permission 可选：工具权限闸配置；缺省时由消费方（如 TUI）决定默认模式与内置规则。
	Permission PermissionConfig `json:"permission"`
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
