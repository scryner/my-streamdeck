package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	configDirName     = ".my-streamdeck"
	configFileName    = "config.yaml"
	templateFileName  = "config.yaml.template"
	defaultQuiBaseURL = "https://qui.meoru.duckdns.org"
)

type Config struct {
	Widgets  []WidgetConfig      `yaml:"widgets"`
	Settings []map[string]string `yaml:"settings"`
}

type WidgetConfig struct {
	Type      string `yaml:"type"`
	First     string `yaml:"first,omitempty"`
	Interface string `yaml:"interface,omitempty"`
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, configDirName), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func TemplatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, templateFileName), nil
}

func DefaultConfig() Config {
	return Config{
		Widgets: []WidgetConfig{
			{Type: "clock", First: "analog"},
			{Type: "calendar"},
			{Type: "sysstat"},
			{Type: "caffeinate"},
		},
	}
}

func LoadConfig() (Config, bool, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, false, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), false, nil
		}
		return Config{}, true, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, true, nil
}

func (c Config) SettingsMap() map[string]string {
	out := make(map[string]string, len(c.Settings))
	for _, item := range c.Settings {
		for key, value := range item {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			out[trimmedKey] = strings.TrimSpace(value)
		}
	}
	return out
}

func WriteTemplate() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}

	path := filepath.Join(dir, templateFileName)
	if err := os.WriteFile(path, []byte(configTemplate), 0o644); err != nil {
		return "", fmt.Errorf("write config template: %w", err)
	}

	return path, nil
}

const configTemplate = `widgets:
  - type: clock
    first: analog
  - type: calendar
  - type: sysstat
  - type: network
    interface: en0
  - type: weather.today
  - type: weather.forecast
  - type: caffeinate
  - type: qui
settings:
  - weather.location: INPUT-YOUR-WEATHER-LOCATION
  - qui.access_token: INPUT-YOUR-QUI-ACCESS-TOKEN
`
