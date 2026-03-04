// Package rules provides YAML rule parsing and a rule evaluation engine
// for matching Claude Code hook events against supervision rules.
package rules

import (
	"fmt"
	"regexp"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// ruleFile represents the top-level structure of a YAML rule file.
type ruleFile struct {
	Rules []types.Rule `yaml:"rules"`
}

// LoadRules loads all .yaml files from the given directory and returns the
// combined set of parsed rules. Files are read in lexicographic order.
func LoadRules(dir string) ([]types.Rule, error) {
	files, err := listYAMLFiles(dir)
	if err != nil {
		return nil, err
	}

	var allRules []types.Rule
	for _, path := range files {
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
	rf, err := readRuleFile(path)
	if err != nil {
		return nil, err
	}

	// Apply defaults and validate parsed rules.
	for i := range rf.Rules {
		if rf.Rules[i].Trigger.Logic == "" {
			rf.Rules[i].Trigger.Logic = "and"
		}
		if err := validateDurations(&rf.Rules[i]); err != nil {
			return nil, fmt.Errorf("rule %q in %q: %w", rf.Rules[i].Name, path, err)
		}
		if err := validateConditionFields(&rf.Rules[i]); err != nil {
			return nil, fmt.Errorf("rule %q in %q: %w", rf.Rules[i].Name, path, err)
		}
	}

	return rf.Rules, nil
}

// validHashModes lists all supported hash_mode values.
var validHashModes = map[string]bool{
	"":           true,
	"content":    true,
	"edit":       true,
	"command":    true,
	"known_file": true,
}

// validateConditionFields checks that hash_mode, diff_pattern, and
// diff_shrink_ratio values are valid.
func validateConditionFields(rule *types.Rule) error {
	for j, cond := range rule.Trigger.Conditions {
		if !validHashModes[cond.HashMode] {
			return fmt.Errorf("condition %d: invalid hash_mode %q", j, cond.HashMode)
		}
		if cond.DiffPattern != "" {
			if _, err := regexp.Compile(cond.DiffPattern); err != nil {
				return fmt.Errorf("condition %d: invalid diff_pattern %q: %w", j, cond.DiffPattern, err)
			}
		}
		if cond.DiffShrinkRatio != 0 && (cond.DiffShrinkRatio < 0 || cond.DiffShrinkRatio > 1) {
			return fmt.Errorf("condition %d: diff_shrink_ratio must be between 0 and 1, got %g", j, cond.DiffShrinkRatio)
		}
	}
	return nil
}

// validateDurations checks that all duration strings in a rule are parseable.
func validateDurations(rule *types.Rule) error {
	for j, cond := range rule.Trigger.Conditions {
		if cond.Within != "" {
			if _, err := time.ParseDuration(cond.Within); err != nil {
				return fmt.Errorf("condition %d: invalid within duration %q: %w", j, cond.Within, err)
			}
		}
	}
	if rule.Action.Cooldown != "" {
		if _, err := time.ParseDuration(rule.Action.Cooldown); err != nil {
			return fmt.Errorf("invalid cooldown duration %q: %w", rule.Action.Cooldown, err)
		}
	}
	return nil
}
