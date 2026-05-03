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
