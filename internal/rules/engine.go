package rules

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jacklin/squawk/internal/types"
)

// Engine evaluates loaded rules against activity history and current events.
type Engine struct {
	rules []types.Rule
}

// NewEngine creates a new rule evaluation engine with the given rules.
func NewEngine(rules []types.Rule) *Engine {
	return &Engine{rules: rules}
}

// Rules returns the engine's loaded rules.
func (e *Engine) Rules() []types.Rule {
	return e.rules
}

// Evaluate checks all enabled rules against the activity history and current
// event. It returns any rules that matched, sorted by priority (highest first).
func (e *Engine) Evaluate(activities []types.Activity, currentEvent types.Event) []types.RuleMatch {
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
	logic := rule.Trigger.Logic
	if logic == "" {
		logic = "and"
	}

	var allMatched []types.Activity

	switch logic {
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
		// Compile the tool pattern; anchor it for full-match semantics.
		toolRe, _ = regexp.Compile("^(?:" + cond.Tool + ")$")
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

// appendUnique appends activities from src to dst, skipping duplicates
// (compared by timestamp and session ID).
func appendUnique(dst, src []types.Activity) []types.Activity {
	seen := make(map[string]bool, len(dst))
	for _, a := range dst {
		key := a.SessionID + a.Timestamp.String()
		seen[key] = true
	}
	for _, a := range src {
		key := a.SessionID + a.Timestamp.String()
		if !seen[key] {
			dst = append(dst, a)
			seen[key] = true
		}
	}
	return dst
}

// sortMatchesByPriority sorts matches in descending priority order.
func sortMatchesByPriority(matches []types.RuleMatch) {
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].Rule.Priority > matches[j-1].Rule.Priority; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
}
