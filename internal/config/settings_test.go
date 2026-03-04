package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

func TestInstallHooks_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)
	assertSquawkHooksPresent(t, settings, 3131)
}

func TestInstallHooks_ExistingSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write existing settings with permissions and model.
	existing := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Glob"},
		},
		"model": "claude-sonnet-4-6",
	}
	writeTestSettings(t, path, existing)

	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)

	// Squawk hooks should be present.
	assertSquawkHooksPresent(t, settings, 3131)

	// Existing content should be preserved.
	if settings["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v, want claude-sonnet-4-6", settings["model"])
	}
	if settings["permissions"] == nil {
		t.Error("permissions not preserved")
	}
}

func TestInstallHooks_ExistingNonSquawkHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write existing settings with non-squawk hooks.
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "/usr/local/bin/my-guard",
						},
					},
				},
			},
		},
	}
	writeTestSettings(t, path, existing)

	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)

	// Squawk hooks should be added.
	assertSquawkHooksPresent(t, settings, 3131)

	// Non-squawk hooks should be preserved.
	hooks := settings["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)
	// Should have both the non-squawk hook and the squawk hook.
	if len(preToolUse) != 2 {
		t.Errorf("PreToolUse entries = %d, want 2", len(preToolUse))
	}
}

func TestInstallHooks_UpgradeOldSquawkHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write old curl-based squawk hooks.
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Edit|Write|Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "curl -s http://localhost:3131/hooks/pre-tool-use",
						},
					},
				},
			},
		},
	}
	writeTestSettings(t, path, existing)

	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)

	// Old hooks should be replaced, not duplicated.
	hooks := settings["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)
	if len(preToolUse) != 1 {
		t.Errorf("PreToolUse entries = %d, want 1 (old hook should be replaced)", len(preToolUse))
	}

	// The new hook should be HTTP type.
	entry := preToolUse[0].(map[string]any)
	innerHooks := entry["hooks"].([]any)
	innerHook := innerHooks[0].(map[string]any)
	if innerHook["type"] != "http" {
		t.Errorf("hook type = %v, want http", innerHook["type"])
	}
}

func TestInstallHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("first InstallHooks() error: %v", err)
	}
	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("second InstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)
	hooks := settings["hooks"].(map[string]any)

	// Should not duplicate entries.
	for _, eventType := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		entries := hooks[eventType].([]any)
		if len(entries) != 1 {
			t.Errorf("%s entries = %d, want 1", eventType, len(entries))
		}
	}
}

func TestUninstallHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Install then uninstall.
	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}
	if err := UninstallHooks(path, 3131); err != nil {
		t.Fatalf("UninstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)

	// Hooks section should be removed entirely.
	if _, ok := settings["hooks"]; ok {
		t.Error("hooks section should be removed after uninstall")
	}
}

func TestUninstallHooks_PreservesOtherHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write settings with both squawk and non-squawk hooks.
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/my-guard"},
					},
				},
				map[string]any{
					"matcher": "Edit|Write|Bash",
					"hooks": []any{
						map[string]any{"type": "http", "url": "http://localhost:3131/hooks/pre-tool-use", "timeout": 5},
					},
				},
			},
		},
	}
	writeTestSettings(t, path, existing)

	if err := UninstallHooks(path, 3131); err != nil {
		t.Fatalf("UninstallHooks() error: %v", err)
	}

	settings := readTestSettings(t, path)
	hooks := settings["hooks"].(map[string]any)
	preToolUse := hooks["PreToolUse"].([]any)

	if len(preToolUse) != 1 {
		t.Fatalf("PreToolUse entries = %d, want 1", len(preToolUse))
	}

	// The remaining hook should be the non-squawk one.
	entry := preToolUse[0].(map[string]any)
	innerHooks := entry["hooks"].([]any)
	innerHook := innerHooks[0].(map[string]any)
	if innerHook["command"] != "/usr/local/bin/my-guard" {
		t.Errorf("remaining hook command = %v, want /usr/local/bin/my-guard", innerHook["command"])
	}
}

func TestUninstallHooks_NoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Should not error on missing file.
	if err := UninstallHooks(path, 3131); err != nil {
		t.Fatalf("UninstallHooks() on missing file error: %v", err)
	}
}

func TestIsHooksInstalled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Not installed yet.
	installed, err := IsHooksInstalled(path, 3131)
	if err != nil {
		t.Fatalf("IsHooksInstalled() error: %v", err)
	}
	if installed {
		t.Error("should not be installed on missing file")
	}

	// Install.
	if err := InstallHooks(path, 3131, nil); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	installed, err = IsHooksInstalled(path, 3131)
	if err != nil {
		t.Fatalf("IsHooksInstalled() error: %v", err)
	}
	if !installed {
		t.Error("should be installed after InstallHooks")
	}

	// Uninstall.
	if err := UninstallHooks(path, 3131); err != nil {
		t.Fatalf("UninstallHooks() error: %v", err)
	}

	installed, err = IsHooksInstalled(path, 3131)
	if err != nil {
		t.Fatalf("IsHooksInstalled() error: %v", err)
	}
	if installed {
		t.Error("should not be installed after UninstallHooks")
	}
}

func TestSettingsPath(t *testing.T) {
	// Test with custom CLAUDE_CONFIG_DIR.
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/custom-claude")
	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath() error: %v", err)
	}
	if path != "/tmp/custom-claude/settings.json" {
		t.Errorf("path = %q, want /tmp/custom-claude/settings.json", path)
	}
}

func TestSettingsPath_Default(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".claude", "settings.json")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// Helpers

func readTestSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	return settings
}

func writeTestSettings(t *testing.T, path string, settings map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write settings: %v", err)
	}
}

func assertSquawkHooksPresent(t *testing.T, settings map[string]any, port int) {
	t.Helper()
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks section not found")
	}
	for _, eventType := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		entries, ok := hooks[eventType].([]any)
		if !ok || len(entries) == 0 {
			t.Errorf("%s hooks not found", eventType)
		}
	}
}

func TestPreToolUseMatcher(t *testing.T) {
	tests := []struct {
		name  string
		rules []types.Rule
		want  string
	}{
		{
			name:  "no rules returns defaults",
			rules: nil,
			want:  "Bash|Edit|Write",
		},
		{
			name: "adds tools from block rules",
			rules: []types.Rule{
				{
					Enabled: true,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Event: "PreToolUse", Tool: "Read|Glob"},
					}},
					Action: types.Action{Type: types.ActionBlock},
				},
			},
			want: "Bash|Edit|Glob|Read|Write",
		},
		{
			name: "ignores disabled rules",
			rules: []types.Rule{
				{
					Enabled: false,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Event: "PreToolUse", Tool: "Read"},
					}},
					Action: types.Action{Type: types.ActionBlock},
				},
			},
			want: "Bash|Edit|Write",
		},
		{
			name: "ignores non-block actions",
			rules: []types.Rule{
				{
					Enabled: true,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Tool: "Read"},
					}},
					Action: types.Action{Type: types.ActionInject},
				},
			},
			want: "Bash|Edit|Write",
		},
		{
			name: "ignores regex metacharacters",
			rules: []types.Rule{
				{
					Enabled: true,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Tool: "Edit|Write.*"},
					}},
					Action: types.Action{Type: types.ActionBlock},
				},
			},
			want: "Bash|Edit|Write",
		},
		{
			name: "adds tools from action tool_scope",
			rules: []types.Rule{
				{
					Enabled: true,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Event: "PostToolUse", Tool: "Read"},
					}},
					Action: types.Action{Type: types.ActionBlock, ToolScope: "Read|Grep"},
				},
			},
			want: "Bash|Edit|Grep|Read|Write",
		},
		{
			name: "condition with empty event includes tool",
			rules: []types.Rule{
				{
					Enabled: true,
					Trigger: types.Trigger{Conditions: []types.Condition{
						{Tool: "Grep"},
					}},
					Action: types.Action{Type: types.ActionBlock},
				},
			},
			want: "Bash|Edit|Grep|Write",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PreToolUseMatcher(tt.rules)
			if got != tt.want {
				t.Errorf("PreToolUseMatcher() = %q, want %q", got, tt.want)
			}
		})
	}
}
