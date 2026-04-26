package config

import "testing"

func TestNewConfig(t *testing.T) {
	config, err := NewConfig("config.json")
	if err != nil {
		t.Errorf("Error creating config: %v", err)
	}
	t.Logf("Config: %+v", config)
}
