// Package rules provides YAML rule parsing and a rule evaluation engine
// for matching Claude Code hook events against supervision rules.
package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacklin/squawk/internal/types"
	"gopkg.in/yaml.v3"
)

// ruleFile represents the top-level structure of a YAML rule file.
type ruleFile struct {
	Rules []types.Rule `yaml:"rules"`
}

// LoadRules loads all .yaml files from the given directory and returns the
// combined set of parsed rules. Files are read in lexicographic order.
func LoadRules(dir string) ([]types.Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules directory %q: %w", dir, err)
	}

	var allRules []types.Rule
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		rules, err := ParseRuleFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse rule file %q: %w", path, err)
		}
		allRules = append(allRules, rules...)
	}
	return allRules, nil
}

// ParseRuleFile parses a single YAML rule file and returns the rules it contains.
func ParseRuleFile(path string) ([]types.Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}

	var rf ruleFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML from %q: %w", path, err)
	}

	// Apply defaults for parsed rules.
	for i := range rf.Rules {
		if rf.Rules[i].Trigger.Logic == "" {
			rf.Rules[i].Trigger.Logic = "and"
		}
	}

	return rf.Rules, nil
}
