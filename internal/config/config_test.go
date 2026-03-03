package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"host", cfg.Server.Host, "localhost"},
		{"port", cfg.Server.Port, 3131},
		{"rules_dir", cfg.RulesDir, "./rules"},
		{"log_file", cfg.LogFile, ".squawk/squawk.log"},
		{"log_level", cfg.LogLevel, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &types.Config{
		Server: types.ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		RulesDir: "/custom/rules",
		LogFile:  "/var/log/squawk.log",
		LogLevel: "debug",
	}

	if err := Save(original, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"host", loaded.Server.Host, original.Server.Host},
		{"port", loaded.Server.Port, original.Server.Port},
		{"rules_dir", loaded.RulesDir, original.RulesDir},
		{"log_file", loaded.LogFile, original.LogFile},
		{"log_level", loaded.LogLevel, original.LogLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "config.yaml")

	cfg := Default()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to exist after Save")
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error when loading nonexistent file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when loading invalid YAML")
	}
}

func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.yaml")

	content := []byte("server:\n  port: 9999\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Port should be overridden by the file.
	if cfg.Server.Port != 9999 {
		t.Errorf("port: got %d, want 9999", cfg.Server.Port)
	}

	// Host should retain the default since the file didn't specify it.
	if cfg.Server.Host != "localhost" {
		t.Errorf("host: got %q, want %q", cfg.Server.Host, "localhost")
	}

	// RulesDir should retain the default.
	if cfg.RulesDir != "./rules" {
		t.Errorf("rules_dir: got %q, want %q", cfg.RulesDir, "./rules")
	}
}

func TestGenerateHooksConfig(t *testing.T) {
	hooks, err := GenerateHooksConfig(3131)
	if err != nil {
		t.Fatalf("GenerateHooksConfig() error: %v", err)
	}

	hooksMap, ok := hooks["hooks"].(map[string]any)
	if !ok {
		t.Fatal("expected 'hooks' key with map value")
	}

	expectedKeys := []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"}
	for _, key := range expectedKeys {
		if _, ok := hooksMap[key]; !ok {
			t.Errorf("missing hook key %q", key)
		}
	}

	// Verify PreToolUse has the correct matcher.
	preToolUse, ok := hooksMap["PreToolUse"].([]map[string]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("expected PreToolUse to be a non-empty slice")
	}
	if matcher, ok := preToolUse[0]["matcher"].(string); !ok || matcher != "Edit|Write|Bash" {
		t.Errorf("PreToolUse matcher: got %q, want %q", matcher, "Edit|Write|Bash")
	}

	// Verify PreToolUse hook uses HTTP type with correct URL.
	preInnerHooks, ok := preToolUse[0]["hooks"].([]map[string]any)
	if !ok || len(preInnerHooks) == 0 {
		t.Fatal("expected PreToolUse hooks to be a non-empty slice")
	}
	if hookType, ok := preInnerHooks[0]["type"].(string); !ok || hookType != "http" {
		t.Errorf("PreToolUse hook type: got %q, want %q", hookType, "http")
	}
	if url, ok := preInnerHooks[0]["url"].(string); !ok || url != "http://localhost:3131/hooks/pre-tool-use" {
		t.Errorf("PreToolUse URL: got %q, want %q", url, "http://localhost:3131/hooks/pre-tool-use")
	}

	// Verify PostToolUse hook uses HTTP type with correct URL.
	postToolUse, ok := hooksMap["PostToolUse"].([]map[string]any)
	if !ok || len(postToolUse) == 0 {
		t.Fatal("expected PostToolUse to be a non-empty slice")
	}
	postInnerHooks, ok := postToolUse[0]["hooks"].([]map[string]any)
	if !ok || len(postInnerHooks) == 0 {
		t.Fatal("expected PostToolUse hooks to be a non-empty slice")
	}
	if url, ok := postInnerHooks[0]["url"].(string); !ok || url != "http://localhost:3131/hooks/post-tool-use" {
		t.Errorf("PostToolUse URL: got %q, want %q", url, "http://localhost:3131/hooks/post-tool-use")
	}
}

func TestGenerateHooksConfigInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too_high", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateHooksConfig(tt.port)
			if err == nil {
				t.Errorf("expected error for port %d", tt.port)
			}
		})
	}
}

func TestGenerateHooksConfigCustomPort(t *testing.T) {
	hooks, err := GenerateHooksConfig(8080)
	if err != nil {
		t.Fatalf("GenerateHooksConfig() error: %v", err)
	}

	hooksMap := hooks["hooks"].(map[string]any)
	preToolUse := hooksMap["PreToolUse"].([]map[string]any)
	innerHooks := preToolUse[0]["hooks"].([]map[string]any)
	url, ok := innerHooks[0]["url"].(string)
	if !ok {
		t.Fatal("expected url to be a string")
	}

	if want := "http://localhost:8080/hooks/pre-tool-use"; url != want {
		t.Errorf("url: got %q, want %q", url, want)
	}
}

func TestGenerateScriptHooksConfig(t *testing.T) {
	hooks, err := GenerateScriptHooksConfig(3131, "/usr/local/bin/hook.sh")
	if err != nil {
		t.Fatalf("GenerateScriptHooksConfig() error: %v", err)
	}

	hooksMap, ok := hooks["hooks"].(map[string]any)
	if !ok {
		t.Fatal("expected 'hooks' key with map value")
	}

	expectedKeys := []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"}
	for _, key := range expectedKeys {
		if _, ok := hooksMap[key]; !ok {
			t.Errorf("missing hook key %q", key)
		}
	}

	// Verify PreToolUse uses command type with script path.
	preToolUse := hooksMap["PreToolUse"].([]map[string]any)
	innerHooks := preToolUse[0]["hooks"].([]map[string]any)
	cmd, ok := innerHooks[0]["command"].(string)
	if !ok {
		t.Fatal("expected command to be a string")
	}
	if want := "/usr/local/bin/hook.sh PreToolUse"; cmd != want {
		t.Errorf("command: got %q, want %q", cmd, want)
	}
}

func TestGenerateScriptHooksConfigCustomPort(t *testing.T) {
	hooks, err := GenerateScriptHooksConfig(8080, "./scripts/hook.sh")
	if err != nil {
		t.Fatalf("GenerateScriptHooksConfig() error: %v", err)
	}

	hooksMap := hooks["hooks"].(map[string]any)
	preToolUse := hooksMap["PreToolUse"].([]map[string]any)
	innerHooks := preToolUse[0]["hooks"].([]map[string]any)
	cmd := innerHooks[0]["command"].(string)

	if want := "SQUAWK_PORT=8080 ./scripts/hook.sh PreToolUse"; cmd != want {
		t.Errorf("command: got %q, want %q", cmd, want)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
