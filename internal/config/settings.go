package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SettingsPath returns the path to Claude Code's settings.json.
// It respects the CLAUDE_CONFIG_DIR environment variable.
func SettingsPath() (string, error) {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "settings.json"), nil
}

// InstallHooks reads settings.json, merges squawk HTTP hooks, and writes it
// back. Existing squawk hooks (identified by URL containing localhost:<port>)
// are replaced. All other content is preserved. The file is created if it does
// not exist. Writes are atomic via temp file + rename.
func InstallHooks(settingsPath string, port int) error {
	settings, err := readSettings(settingsPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	hooks := ensureHooksMap(settings)
	squawkHooks := buildSquawkHooks(port)

	for eventType, newEntries := range squawkHooks {
		existing := getHookEntries(hooks, eventType)
		filtered := filterNonSquawk(existing, port)
		merged := append(filtered, newEntries...)
		hooks[eventType] = merged
	}

	settings["hooks"] = hooks
	return writeSettings(settingsPath, settings)
}

// UninstallHooks removes squawk hooks from settings.json, preserving all
// other content. Returns nil if the file does not exist.
func UninstallHooks(settingsPath string, port int) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return nil
	}
	hooks, ok := hooksRaw.(map[string]any)
	if !ok {
		return nil
	}

	for eventType := range hooks {
		existing := getHookEntries(hooks, eventType)
		filtered := filterNonSquawk(existing, port)
		if len(filtered) == 0 {
			delete(hooks, eventType)
		} else {
			hooks[eventType] = filtered
		}
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	return writeSettings(settingsPath, settings)
}

// IsHooksInstalled checks whether squawk hooks are present in settings.json.
func IsHooksInstalled(settingsPath string, port int) (bool, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	hooksRaw, ok := settings["hooks"]
	if !ok {
		return false, nil
	}
	hooks, ok := hooksRaw.(map[string]any)
	if !ok {
		return false, nil
	}

	for _, eventType := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		entries := getHookEntries(hooks, eventType)
		for _, entry := range entries {
			if isSquawkHook(entry, port) {
				return true, nil
			}
		}
	}

	return false, nil
}

// readSettings reads and parses settings.json. Returns an empty map and
// fs.ErrNotExist (wrapped) if the file does not exist.
func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return make(map[string]any), err
		}
		return nil, fmt.Errorf("failed to read settings file %q: %w", path, err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings file %q: %w", path, err)
	}
	return settings, nil
}

// writeSettings atomically writes settings.json via temp file + rename.
func writeSettings(path string, settings map[string]any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "settings-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp settings file: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temp settings file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp settings file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp settings file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename temp settings file: %w", err)
	}

	committed = true
	return nil
}

// ensureHooksMap returns the "hooks" map from settings, creating it if needed.
func ensureHooksMap(settings map[string]any) map[string]any {
	hooksRaw, ok := settings["hooks"]
	if !ok {
		return make(map[string]any)
	}
	hooks, ok := hooksRaw.(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return hooks
}

// getHookEntries extracts hook entries for a given event type as a slice of maps.
func getHookEntries(hooks map[string]any, eventType string) []any {
	raw, ok := hooks[eventType]
	if !ok {
		return nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil
	}
	return entries
}

// filterNonSquawk returns entries that don't belong to squawk.
func filterNonSquawk(entries []any, port int) []any {
	var result []any
	for _, entry := range entries {
		if !isSquawkHook(entry, port) {
			result = append(result, entry)
		}
	}
	return result
}

// isSquawkHook checks if a hook entry belongs to squawk by looking for
// localhost:<port> in URL or command fields.
func isSquawkHook(entry any, port int) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}

	needle := fmt.Sprintf("localhost:%d", port)

	// Check nested hooks array (HTTP hook format).
	if hooksRaw, ok := m["hooks"]; ok {
		if hooks, ok := hooksRaw.([]any); ok {
			for _, h := range hooks {
				if hm, ok := h.(map[string]any); ok {
					if url, ok := hm["url"].(string); ok && strings.Contains(url, needle) {
						return true
					}
					if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, needle) {
						return true
					}
				}
			}
		}
	}

	// Check top-level url/command (flat format).
	if url, ok := m["url"].(string); ok && strings.Contains(url, needle) {
		return true
	}
	if cmd, ok := m["command"].(string); ok && strings.Contains(cmd, needle) {
		return true
	}

	return false
}

// buildSquawkHooks returns the squawk hook entries keyed by event type.
func buildSquawkHooks(port int) map[string][]any {
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	makeEntry := func(matcher, endpoint string) any {
		return map[string]any{
			"matcher": matcher,
			"hooks": []any{
				map[string]any{
					"type":    "http",
					"url":     fmt.Sprintf("%s%s", baseURL, endpoint),
					"timeout": 5,
				},
			},
		}
	}

	return map[string][]any{
		"PreToolUse":         {makeEntry("Edit|Write|Bash", "/hooks/pre-tool-use")},
		"PostToolUse":        {makeEntry("", "/hooks/post-tool-use")},
		"PostToolUseFailure": {makeEntry("", "/hooks/post-tool-use")},
	}
}
