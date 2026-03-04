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
	hooks, err := GenerateHooksConfig(3131, nil)
	if err != nil {
		t.Fatalf("GenerateHooksConfig() error: %v", err)
	}

	hooksMap, ok := hooks["hooks"].(map[string][]any)
	if !ok {
		t.Fatal("expected 'hooks' key with map[string][]any value")
	}

	for _, key := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		if _, ok := hooksMap[key]; !ok {
			t.Errorf("missing hook key %q", key)
		}
	}

	// Verify PreToolUse has the correct default matcher and URL.
	preEntries := hooksMap["PreToolUse"]
	if len(preEntries) == 0 {
		t.Fatal("expected PreToolUse to be non-empty")
	}
	pre, ok := preEntries[0].(map[string]any)
	if !ok {
		t.Fatal("expected PreToolUse entry to be map[string]any")
	}
	if matcher, _ := pre["matcher"].(string); matcher != "Bash|Edit|Write" {
		t.Errorf("PreToolUse matcher: got %q, want %q", matcher, "Bash|Edit|Write")
	}
	preInner, ok := pre["hooks"].([]any)
	if !ok || len(preInner) == 0 {
		t.Fatal("expected PreToolUse hooks to be a non-empty slice")
	}
	preHook, _ := preInner[0].(map[string]any)
	if hookType, _ := preHook["type"].(string); hookType != "http" {
		t.Errorf("PreToolUse hook type: got %q, want %q", hookType, "http")
	}
	if url, _ := preHook["url"].(string); url != "http://localhost:3131/hooks/pre-tool-use" {
		t.Errorf("PreToolUse URL: got %q, want %q", url, "http://localhost:3131/hooks/pre-tool-use")
	}

	// Verify PostToolUse hook URL.
	postEntries := hooksMap["PostToolUse"]
	if len(postEntries) == 0 {
		t.Fatal("expected PostToolUse to be non-empty")
	}
	post, _ := postEntries[0].(map[string]any)
	postInner, ok := post["hooks"].([]any)
	if !ok || len(postInner) == 0 {
		t.Fatal("expected PostToolUse hooks to be a non-empty slice")
	}
	postHook, _ := postInner[0].(map[string]any)
	if url, _ := postHook["url"].(string); url != "http://localhost:3131/hooks/post-tool-use" {
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
			_, err := GenerateHooksConfig(tt.port, nil)
			if err == nil {
				t.Errorf("expected error for port %d", tt.port)
			}
		})
	}
}

func TestGenerateHooksConfigCustomPort(t *testing.T) {
	hooks, err := GenerateHooksConfig(8080, nil)
	if err != nil {
		t.Fatalf("GenerateHooksConfig() error: %v", err)
	}

	hooksMap := hooks["hooks"].(map[string][]any)
	preEntries := hooksMap["PreToolUse"]
	pre, _ := preEntries[0].(map[string]any)
	inner, _ := pre["hooks"].([]any)
	hook, _ := inner[0].(map[string]any)
	url, _ := hook["url"].(string)

	if want := "http://localhost:8080/hooks/pre-tool-use"; url != want {
		t.Errorf("url: got %q, want %q", url, want)
	}
}

