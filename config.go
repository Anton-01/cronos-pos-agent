package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const AgentVersion = "1.2.0"

type Config struct {
	APIToken       string   `json:"api_token"`
	AllowedOrigins []string `json:"allowed_origins"`
	UpdateURL      string   `json:"update_url"`
	Port           int      `json:"port"`
}

var defaultOrigins = []string{
	"https://pos-app.tech",
	"http://localhost:3000",
	"http://localhost:5173",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:5173",
}

const defaultUpdateURL = "https://pos-app.tech/agent/version.json"

var (
	appConfig     Config
	configOnce    sync.Once
	configLoadErr error
)

func LoadConfig() (Config, error) {
	configOnce.Do(func() {
		configPath := configFilePath()
		needsWrite := false

		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := json.Unmarshal(data, &appConfig); err != nil {
				configLoadErr = fmt.Errorf("config.json corrupto: %w", err)
				return
			}
		}

		if appConfig.APIToken == "" {
			token, err := generateUUID()
			if err != nil {
				configLoadErr = fmt.Errorf("error generando token: %w", err)
				return
			}
			appConfig.APIToken = token
			needsWrite = true
		}

		if len(appConfig.AllowedOrigins) == 0 {
			appConfig.AllowedOrigins = defaultOrigins
			needsWrite = true
		}

		if appConfig.UpdateURL == "" {
			appConfig.UpdateURL = defaultUpdateURL
			needsWrite = true
		}

		if appConfig.Port == 0 {
			appConfig.Port = defaultPort
			needsWrite = true
		}

		if needsWrite {
			if err := saveConfig(configPath); err != nil {
				configLoadErr = err
			}
		}
	})

	return appConfig, configLoadErr
}

func saveConfig(path string) error {
	jsonData, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando config: %w", err)
	}
	if err := os.WriteFile(path, jsonData, 0600); err != nil {
		return fmt.Errorf("error escribiendo config.json: %w", err)
	}
	return nil
}

func configFilePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func agentDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func generateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
