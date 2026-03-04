package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
	"gopkg.in/yaml.v3"
)

// EnableRule finds a rule by name across YAML files in dir, sets enabled: true,
// and writes the file back. Returns the file path that was modified.
func EnableRule(dir, name string) (string, error) {
	return setRuleEnabled(dir, name, true)
}

// DisableRule finds a rule by name across YAML files in dir, sets enabled: false,
// and writes the file back. Returns the file path that was modified.
func DisableRule(dir, name string) (string, error) {
	return setRuleEnabled(dir, name, false)
}

// RemoveRule removes a rule by name from its YAML file. If the file becomes
// empty (no rules left), it is deleted. Returns the file path and remaining
// rule count.
func RemoveRule(dir, name string) (string, int, error) {
	path, rf, idx, err := locateRule(dir, name)
	if err != nil {
		return "", 0, err
	}

	rf.Rules = append(rf.Rules[:idx], rf.Rules[idx+1:]...)

	if len(rf.Rules) == 0 {
		if err := os.Remove(path); err != nil {
			return path, 0, fmt.Errorf("failed to remove empty rule file %q: %w", path, err)
		}
		return path, 0, nil
	}

	if err := writeRuleFile(path, rf); err != nil {
		return path, len(rf.Rules), err
	}
	return path, len(rf.Rules), nil
}

// FindRule finds a rule by name across all YAML files in dir.
func FindRule(dir, name string) (*types.Rule, string, error) {
	path, rf, idx, err := locateRule(dir, name)
	if err != nil {
		return nil, "", err
	}
	return &rf.Rules[idx], path, nil
}

// setRuleEnabled toggles the enabled field for a named rule.
func setRuleEnabled(dir, name string, enabled bool) (string, error) {
	path, rf, idx, err := locateRule(dir, name)
	if err != nil {
		return "", err
	}

	rf.Rules[idx].Enabled = enabled
	if err := writeRuleFile(path, rf); err != nil {
		return path, err
	}
	return path, nil
}

// locateRule finds a named rule across YAML files in dir, returning the
// file path, parsed file contents, and the rule's index within the file.
func locateRule(dir, name string) (string, *ruleFile, int, error) {
	files, err := listYAMLFiles(dir)
	if err != nil {
		return "", nil, -1, err
	}

	for _, path := range files {
		rf, err := readRuleFile(path)
		if err != nil {
			return "", nil, -1, fmt.Errorf("failed to read rule file %q while locating rule %q: %w", path, name, err)
		}
		for i, r := range rf.Rules {
			if r.Name == name {
				return path, rf, i, nil
			}
		}
	}

	return "", nil, -1, fmt.Errorf("rule %q not found in %q", name, dir)
}

// listYAMLFiles returns sorted YAML file paths from the directory.
func listYAMLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules directory %q: %w", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

// readRuleFile parses a single YAML rule file.
func readRuleFile(path string) (*ruleFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	var rf ruleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("failed to parse file %q: %w", path, err)
	}
	return &rf, nil
}

// writeRuleFile writes the rule file back to disk atomically via temp file + rename.
func writeRuleFile(path string, rf *ruleFile) error {
	data, err := yaml.Marshal(rf)
	if err != nil {
		return fmt.Errorf("failed to marshal rules: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".squawk-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp rule file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp rule file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to sync temp rule file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp rule file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp rule file: %w", err)
	}
	return nil
}
