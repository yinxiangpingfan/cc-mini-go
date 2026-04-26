package config

import (
	"github.com/spf13/viper"
)

//get config from file

type Config struct {
	Provider        string `mapstructure:"provider"`
	APIKey          string `mapstructure:"api_key"`
	BaseUrl         string `mapstructure:"base_url"`
	Model           string `mapstructure:"model"`
	ReasoningEffort string `mapstructure:"reasoning_effort"`
}

func NewConfig(filePath string) (*Config, error) {
	viper.SetConfigFile(filePath)
	err := viper.ReadInConfig()
	if err != nil {
		return nil, err
	}

	var c Config
	viper.Unmarshal(&c)
	return &c, nil
}
