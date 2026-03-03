// Package config provides configuration management for squawk, including
// loading/saving YAML configs and generating Claude Code hooks settings.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"gopkg.in/yaml.v3"
)

// Default returns a Config with sensible default values.
func Default() *types.Config {
	return &types.Config{
		Server: types.ServerConfig{
			Host: "localhost",
			Port: 3131,
		},
		RulesDir: "./rules",
		LogFile:  ".squawk/squawk.log",
		LogLevel: "info",
	}
}

// Load reads a YAML config file at the given path and returns a parsed Config.
func Load(path string) (*types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}

	return cfg, nil
}

// Save writes the given Config to a YAML file at the specified path,
// creating parent directories as needed.
func Save(cfg *types.Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", dir, err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file %q: %w", path, err)
	}

	return nil
}

// GenerateHooksConfig generates a Claude Code hooks settings.json snippet
// using HTTP hooks (preferred). Claude Code POSTs event JSON directly to
// squawk's endpoints. Connection failures and timeouts are non-blocking,
// so Claude Code continues if squawk is not running (fail-open).
func GenerateHooksConfig(port int) (map[string]any, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", port)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	hooks := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "Edit|Write|Bash",
					"hooks": []map[string]any{
						{
							"type":    "http",
							"url":     fmt.Sprintf("%s/hooks/pre-tool-use", baseURL),
							"timeout": 5,
						},
					},
				},
			},
			"PostToolUse": []map[string]any{
				{
					"matcher": "",
					"hooks": []map[string]any{
						{
							"type":    "http",
							"url":     fmt.Sprintf("%s/hooks/post-tool-use", baseURL),
							"timeout": 5,
						},
					},
				},
			},
			"PostToolUseFailure": []map[string]any{
				{
					"matcher": "",
					"hooks": []map[string]any{
						{
							"type":    "http",
							"url":     fmt.Sprintf("%s/hooks/post-tool-use", baseURL),
							"timeout": 5,
						},
					},
				},
			},
		},
	}

	return hooks, nil
}

// GenerateScriptHooksConfig generates a Claude Code hooks settings.json
// snippet using command hooks that invoke scripts/hook.sh. The script reads
// stdin JSON, forwards to squawk, and translates the response into Claude
// Code's expected format. Requires jq to be installed.
func GenerateScriptHooksConfig(port int, scriptPath string) (map[string]any, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port number: %d", port)
	}

	portEnv := ""
	if port != 3131 {
		portEnv = fmt.Sprintf("SQUAWK_PORT=%d ", port)
	}

	hooks := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "Edit|Write|Bash",
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": fmt.Sprintf("%s%s PreToolUse", portEnv, scriptPath),
						},
					},
				},
			},
			"PostToolUse": []map[string]any{
				{
					"matcher": "",
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": fmt.Sprintf("%s%s PostToolUse", portEnv, scriptPath),
						},
					},
				},
			},
			"PostToolUseFailure": []map[string]any{
				{
					"matcher": "",
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": fmt.Sprintf("%s%s PostToolUseFailure", portEnv, scriptPath),
						},
					},
				},
			},
		},
	}

	return hooks, nil
}
