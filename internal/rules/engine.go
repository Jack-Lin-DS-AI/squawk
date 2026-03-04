package rules

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// Engine evaluates loaded rules against activity history and current events.
type Engine struct {
	rules     []types.Rule
	mu        sync.RWMutex
	cooldowns map[string]time.Time // rule name → cooldown expiry
}

// NewEngine creates a new rule evaluation engine with the given rules.
func NewEngine(rules []types.Rule) *Engine {
	return &Engine{
		rules:     rules,
		cooldowns: make(map[string]time.Time),
	}
}

// ReplaceRules atomically replaces the engine's loaded rules, used for
// hot-reload after rule mutations. All cooldowns are cleared because rule
// identities may have changed; in-progress cooldowns will not survive a reload.
func (e *Engine) ReplaceRules(newRules []types.Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = newRules
	e.cooldowns = make(map[string]time.Time)
}

// Evaluate checks all enabled rules against the activity history and current
// event. It returns any rules that matched, sorted by priority (highest first).
func (e *Engine) Evaluate(activities []types.Activity, currentEvent types.Event) []types.RuleMatch {
	e.mu.Lock()
	defer e.mu.Unlock()

	var matches []types.RuleMatch

	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}
		if match, ok := e.evaluateRule(rule, activities, currentEvent); ok {
			matches = append(matches, match)
		}
	}

	// Sort by priority descending (highest first).
	sortMatchesByPriority(matches)
	return matches
}

// evaluateRule checks a single rule against the activity history.
func (e *Engine) evaluateRule(rule types.Rule, activities []types.Activity, currentEvent types.Event) (types.RuleMatch, bool) {
	// Check cooldown: skip this rule if it was triggered recently.
	if rule.Action.Cooldown != "" {
		if expiry, ok := e.cooldowns[rule.Name]; ok && currentEvent.Timestamp.Before(expiry) {
			return types.RuleMatch{}, false
		}
	}

	var allMatched []types.Activity

	// Logic default ("and") is applied by ParseRuleFile at load time.
	switch rule.Trigger.Logic {
	case "or":
		for _, cond := range rule.Trigger.Conditions {
			matched, ok := evaluateCondition(cond, activities, currentEvent)
			if ok {
				allMatched = appendUnique(allMatched, matched)
			}
		}
		if len(allMatched) == 0 {
			return types.RuleMatch{}, false
		}

	default: // "and"
		for _, cond := range rule.Trigger.Conditions {
			matched, ok := evaluateCondition(cond, activities, currentEvent)
			if !ok {
				return types.RuleMatch{}, false
			}
			allMatched = appendUnique(allMatched, matched)
		}
	}

	// Record cooldown expiry for this rule.
	if rule.Action.Cooldown != "" {
		if d, err := time.ParseDuration(rule.Action.Cooldown); err == nil {
			e.cooldowns[rule.Name] = currentEvent.Timestamp.Add(d)
		}
	}

	return types.RuleMatch{
		Rule:       rule,
		Activities: allMatched,
		MatchedAt:  currentEvent.Timestamp,
	}, true
}

// evaluateCondition checks a single condition against the activity history.
// For negated conditions, it returns true when the condition is NOT met.
func evaluateCondition(cond types.Condition, activities []types.Activity, currentEvent types.Event) ([]types.Activity, bool) {
	// Determine the time window.
	window := time.Duration(0)
	if cond.Within != "" {
		d, err := time.ParseDuration(cond.Within)
		if err == nil {
			window = d
		}
	}

	cutoff := time.Time{}
	if window > 0 {
		cutoff = currentEvent.Timestamp.Add(-window)
	}

	// Build a regex for the tool name if specified.
	var toolRe *regexp.Regexp
	if cond.Tool != "" {
		var err error
		toolRe, err = regexp.Compile("^(?:" + cond.Tool + ")$")
		if err != nil {
			// Invalid regex — condition cannot match.
			return nil, false
		}
	}

	// Collect activities that match this condition's basic criteria.
	var matched []types.Activity
	for _, act := range activities {
		if !matchesConditionCriteria(cond, act, cutoff, toolRe) {
			continue
		}
		matched = append(matched, act)
	}

	requiredCount := cond.Count
	if requiredCount == 0 {
		requiredCount = 1
	}

	if cond.Negate {
		// Negated: the condition passes when fewer than requiredCount matches are found.
		if len(matched) < requiredCount {
			return nil, true
		}
		return nil, false
	}

	// Positive: the condition passes when at least requiredCount matches are found.
	if len(matched) >= requiredCount {
		return matched, true
	}
	return nil, false
}

// matchesConditionCriteria checks whether a single activity matches the
// event name, tool regex, file pattern, and time window of a condition.
func matchesConditionCriteria(cond types.Condition, act types.Activity, cutoff time.Time, toolRe *regexp.Regexp) bool {
	// Time window check.
	if !cutoff.IsZero() && act.Timestamp.Before(cutoff) {
		return false
	}

	// Event name check.
	if cond.Event != "" && act.Event.HookEventName != cond.Event {
		return false
	}

	// Tool name regex check.
	if toolRe != nil && !toolRe.MatchString(act.Event.ToolName) {
		return false
	}

	// File pattern (glob) check — include.
	if cond.FilePattern != "" {
		filePath, _ := act.Event.ToolInput["file_path"].(string)
		if filePath == "" {
			return false
		}
		if !matchFilePattern(cond.FilePattern, filePath) {
			return false
		}
	}

	// File pattern (glob) check — exclude (match anything NOT matching this pattern).
	if cond.FilePatternExclude != "" {
		filePath, _ := act.Event.ToolInput["file_path"].(string)
		if filePath == "" {
			// No file_path: tools like Bash/Glob/Grep without file context.
			// Treat as "not matching the exclude pattern" → include this activity.
		} else if matchFilePattern(cond.FilePatternExclude, filePath) {
			return false // File matches the exclude pattern → skip.
		}
	}

	return true
}

// matchFilePattern checks whether the file path matches one or more
// pipe-separated glob patterns. The match is performed against the
// base name of the path.
func matchFilePattern(pattern, filePath string) bool {
	baseName := filepath.Base(filePath)
	patterns := strings.Split(pattern, "|")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if matched, err := filepath.Match(p, baseName); err == nil && matched {
			return true
		}
	}
	return false
}

// activityKey returns a unique key for deduplication. Uses a delimiter and
// UnixNano to avoid collisions from string concatenation.
func activityKey(a types.Activity) string {
	return fmt.Sprintf("%s|%d", a.SessionID, a.Timestamp.UnixNano())
}

// appendUnique appends activities from src to dst, skipping duplicates.
func appendUnique(dst, src []types.Activity) []types.Activity {
	seen := make(map[string]bool, len(dst))
	for _, a := range dst {
		seen[activityKey(a)] = true
	}
	for _, a := range src {
		k := activityKey(a)
		if !seen[k] {
			dst = append(dst, a)
			seen[k] = true
		}
	}
	return dst
}

// sortMatchesByPriority sorts matches in descending priority order.
func sortMatchesByPriority(matches []types.RuleMatch) {
	slices.SortFunc(matches, func(a, b types.RuleMatch) int {
		return b.Rule.Priority - a.Rule.Priority
	})
}
