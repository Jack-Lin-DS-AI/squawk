package rules

import (
	"fmt"
	"hash/fnv"
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
			matched, ok := evaluateCondition(cond, activities, activities, currentEvent, nil)
			if ok {
				allMatched = appendUnique(allMatched, matched)
			}
		}
		if len(allMatched) == 0 {
			return types.RuleMatch{}, false
		}

	default: // "and"
		condMatched := make([][]types.Activity, len(rule.Trigger.Conditions))
		for i, cond := range rule.Trigger.Conditions {
			var sourceFiles []string
			if cond.SourceOf != nil {
				ref := *cond.SourceOf
				if ref >= 0 && ref < i {
					sourceFiles = deriveSourceFiles(condMatched[ref])
				}
			}
			matched, ok := evaluateCondition(cond, activities, activities, currentEvent, sourceFiles)
			if !ok {
				return types.RuleMatch{}, false
			}
			condMatched[i] = matched
			allMatched = appendUnique(allMatched, matched)
		}
	}

	// Record cooldown expiry for this rule — but only when the action can
	// actually be enforced. Block actions only take effect on PreToolUse;
	// setting cooldown on PostToolUse would waste it on a no-op.
	if rule.Action.Cooldown != "" {
		if rule.Action.Type == types.ActionBlock && currentEvent.HookEventName != "PreToolUse" {
			// Skip cooldown — block wasn't enforced.
		} else if d, err := time.ParseDuration(rule.Action.Cooldown); err == nil {
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
// allActivities is the unfiltered set, needed by known_file hash mode.
// sourceFiles, when non-nil, restricts matches to activities targeting those
// specific file paths (derived from a referenced condition via source_of).
func evaluateCondition(cond types.Condition, activities, allActivities []types.Activity, currentEvent types.Event, sourceFiles []string) ([]types.Activity, bool) {
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

	// Use pre-compiled regex for tool name matching.
	toolRe := cond.ToolRe

	// Collect activities that match this condition's basic criteria.
	var matched []types.Activity
	for _, act := range activities {
		if !matchesConditionCriteria(cond, act, cutoff, toolRe) {
			continue
		}
		matched = append(matched, act)
	}

	// Apply source file filter if source_of derived file paths are provided.
	if len(sourceFiles) > 0 {
		matched = sourceFileFilter(matched, sourceFiles)
	}

	// Apply hash filter if hash_mode is set.
	if cond.HashMode != "" {
		matched = hashFilterActivities(cond.HashMode, matched, allActivities)
	}

	// Apply diff filter if diff_pattern or diff_shrink_ratio is set.
	if cond.DiffPatternRe != nil || cond.DiffShrinkRatio != 0 {
		matched = diffFilterActivities(cond.DiffPatternRe, cond.DiffShrinkRatio, matched)
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

// --- Hash filter helpers ---

// hashFilterActivities applies hash-based deduplication/detection to matched
// activities. The mode determines the hashing strategy.
func hashFilterActivities(mode string, matched, allActivities []types.Activity) []types.Activity {
	switch mode {
	case "content":
		return contentHashFilter(matched)
	case "edit":
		return groupHashFilter(matched, editHash)
	case "command":
		return groupHashFilter(matched, commandHash)
	case "known_file":
		return knownFileFilter(matched, allActivities)
	default:
		return matched
	}
}

// contentHashFilter detects A→B→A oscillation. For each file, it tracks
// content hashes in order and flags activities whose hash was already seen
// for that file (reversion to a previous state).
func contentHashFilter(activities []types.Activity) []types.Activity {
	// Per-file hash history: file_path → list of hashes seen.
	fileHashes := make(map[string][]uint64)
	var result []types.Activity

	for _, act := range activities {
		filePath, _ := act.Event.ToolInput["file_path"].(string)
		if filePath == "" {
			continue
		}
		h := contentHash(act)
		history := fileHashes[filePath]

		// Check if this hash was already seen (not counting the immediately
		// previous entry — that would be "no change", which is a separate concern).
		seen := false
		for i, prev := range history {
			if prev == h && i < len(history)-1 {
				// Hash matches a non-last entry → content reverted.
				seen = true
				break
			}
		}
		if seen {
			result = append(result, act)
		}
		fileHashes[filePath] = append(history, h)
	}
	return result
}

// groupHashFilter groups activities by a hash function and returns activities
// from groups with 2+ members (i.e., repeated identical operations).
func groupHashFilter(activities []types.Activity, hashFn func(types.Activity) uint64) []types.Activity {
	groups := make(map[uint64][]types.Activity)
	for _, act := range activities {
		h := hashFn(act)
		groups[h] = append(groups[h], act)
	}
	var result []types.Activity
	for _, group := range groups {
		if len(group) >= 2 {
			result = append(result, group...)
		}
	}
	return result
}

// knownFileFilter returns Write activities that target files already seen
// in Read or Edit activities from the full activity set.
func knownFileFilter(matched, allActivities []types.Activity) []types.Activity {
	// Build set of known file paths from Read/Edit activities.
	known := make(map[string]bool)
	for _, act := range allActivities {
		tool := act.Event.ToolName
		if tool == "Read" || tool == "Edit" {
			if fp, ok := act.Event.ToolInput["file_path"].(string); ok && fp != "" {
				known[fp] = true
			}
		}
	}

	// Filter matched (Write) activities to only those targeting known files.
	var result []types.Activity
	for _, act := range matched {
		if fp, ok := act.Event.ToolInput["file_path"].(string); ok && known[fp] {
			result = append(result, act)
		}
	}
	return result
}

// contentHash hashes (file_path, content/new_string) using FNV-1a.
func contentHash(act types.Activity) uint64 {
	h := fnv.New64a()
	fp, _ := act.Event.ToolInput["file_path"].(string)
	h.Write([]byte(fp))
	h.Write([]byte{0}) // delimiter

	// Use new_string (Edit) or content (Write) as the content.
	if ns, ok := act.Event.ToolInput["new_string"].(string); ok {
		h.Write([]byte(ns))
	} else if c, ok := act.Event.ToolInput["content"].(string); ok {
		h.Write([]byte(c))
	}
	return h.Sum64()
}

// editHash hashes (file_path, old_string, new_string) using FNV-1a.
func editHash(act types.Activity) uint64 {
	h := fnv.New64a()
	fp, _ := act.Event.ToolInput["file_path"].(string)
	h.Write([]byte(fp))
	h.Write([]byte{0})
	oldStr, _ := act.Event.ToolInput["old_string"].(string)
	h.Write([]byte(oldStr))
	h.Write([]byte{0})
	ns, _ := act.Event.ToolInput["new_string"].(string)
	h.Write([]byte(ns))
	return h.Sum64()
}

// commandHash hashes the command string from Bash ToolInput using FNV-1a.
func commandHash(act types.Activity) uint64 {
	h := fnv.New64a()
	cmd, _ := act.Event.ToolInput["command"].(string)
	h.Write([]byte(cmd))
	return h.Sum64()
}

// --- Source file derivation helpers ---

// deriveSourceFiles extracts file paths from matched activities and converts
// test file paths to their corresponding source file paths using language
// naming conventions (e.g., calc_test.go → calc.go).
func deriveSourceFiles(activities []types.Activity) []string {
	seen := make(map[string]bool)
	var result []string
	for _, act := range activities {
		fp, _ := act.Event.ToolInput["file_path"].(string)
		if fp == "" {
			continue
		}
		src := testToSourceFile(fp)
		if src != "" && !seen[src] {
			seen[src] = true
			result = append(result, src)
		}
	}
	return result
}

// testToSourceFile converts a test file path to its source counterpart
// using language naming conventions. Returns "" if no convention matches.
func testToSourceFile(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Go: foo_test.go → foo.go
	if strings.HasSuffix(base, "_test.go") {
		return filepath.Join(dir, strings.TrimSuffix(base, "_test.go")+".go")
	}

	// JS/TS: foo.test.ts → foo.ts, foo.spec.tsx → foo.tsx
	for _, mid := range []string{".test.", ".spec."} {
		if idx := strings.LastIndex(base, mid); idx != -1 {
			name := base[:idx]
			ext := base[idx+len(mid)-1:] // includes the dot: ".ts", ".tsx"
			return filepath.Join(dir, name+ext)
		}
	}

	// Python: test_foo.py → foo.py
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return filepath.Join(dir, strings.TrimPrefix(base, "test_"))
	}

	// Python: foo_test.py → foo.py
	if strings.HasSuffix(base, "_test.py") {
		return filepath.Join(dir, strings.TrimSuffix(base, "_test.py")+".py")
	}

	return ""
}

// sourceFileFilter keeps only activities whose file path matches one of the
// derived source files. For Read, matches on file_path. For Grep/Glob,
// matches if the search path is the source file itself or its parent directory.
func sourceFileFilter(activities []types.Activity, sourceFiles []string) []types.Activity {
	var result []types.Activity
	for _, act := range activities {
		if matchesSourceFiles(sourceFiles, act) {
			result = append(result, act)
		}
	}
	return result
}

// matchesSourceFiles checks whether an activity targets any of the given
// source file paths, either directly (file_path) or via search directory (path).
func matchesSourceFiles(sourceFiles []string, act types.Activity) bool {
	// Direct file path match (Read, Edit, Write).
	if fp, ok := act.Event.ToolInput["file_path"].(string); ok && fp != "" {
		for _, sf := range sourceFiles {
			if fp == sf {
				return true
			}
		}
	}

	// Search path match for Grep/Glob: matches if path is the source file
	// itself or its immediate parent directory.
	if p, ok := act.Event.ToolInput["path"].(string); ok && p != "" {
		for _, sf := range sourceFiles {
			if p == sf {
				return true
			}
			if filepath.Dir(sf) == filepath.Clean(p) {
				return true
			}
		}
	}

	return false
}

// --- Diff filter helpers ---

// diffFilterActivities filters Edit activities based on diff analysis.
// If patternRe is set, keeps activities where old_string matches the regex
// but new_string does not (pattern removal). If shrinkRatio > 0, keeps
// activities where len(new_string) < ratio * len(old_string).
func diffFilterActivities(patternRe *regexp.Regexp, shrinkRatio float64, activities []types.Activity) []types.Activity {
	var result []types.Activity
	for _, act := range activities {
		oldStr, _ := act.Event.ToolInput["old_string"].(string)
		newStr, _ := act.Event.ToolInput["new_string"].(string)

		if patternRe != nil {
			if patternRe.MatchString(oldStr) && !patternRe.MatchString(newStr) {
				result = append(result, act)
				continue
			}
		}

		if shrinkRatio > 0 && oldStr != "" {
			if float64(len(newStr)) < shrinkRatio*float64(len(oldStr)) {
				result = append(result, act)
				continue
			}
		}
	}
	return result
}
