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
// that configures PreToolUse and PostToolUse hooks to POST events to the
// squawk server running on the given port.
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
							"type":    "command",
							"command": fmt.Sprintf("curl -s --max-time 5 -X POST %s/hooks/pre-tool-use -H 'Content-Type: application/json' -d @- 2>/dev/null || true", baseURL),
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
							"command": fmt.Sprintf("curl -s --max-time 5 -X POST %s/hooks/post-tool-use -H 'Content-Type: application/json' -d @- 2>/dev/null || true", baseURL),
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
							"command": fmt.Sprintf("curl -s --max-time 5 -X POST %s/hooks/post-tool-use -H 'Content-Type: application/json' -d @- 2>/dev/null || true", baseURL),
						},
					},
				},
			},
		},
	}

	return hooks, nil
}
