package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// --- Helper builders ---

func makeActivity(eventName, toolName string, toolInput map[string]any, ts time.Time, sessionID string) types.Activity {
	return types.Activity{
		Event: types.Event{
			SessionID:     sessionID,
			HookEventName: eventName,
			ToolName:      toolName,
			ToolInput:     toolInput,
			Timestamp:     ts,
		},
		Timestamp: ts,
		SessionID: sessionID,
	}
}

func fileInput(path string) map[string]any {
	return map[string]any{"file_path": path}
}

func editInput(path, old, new string) map[string]any {
	return map[string]any{"file_path": path, "old_string": old, "new_string": new}
}

func writeInput(path, content string) map[string]any {
	return map[string]any{"file_path": path, "content": content}
}

func bashInput(command string) map[string]any {
	return map[string]any{"command": command}
}

// nActivities generates n activities evenly spaced ending at spacing before ref.
// Activity i is at ref - (n-i)*spacing, so oldest first.
func nActivities(n int, event, tool string, input map[string]any, spacing time.Duration, ref time.Time) []types.Activity {
	acts := make([]types.Activity, n)
	for i := range acts {
		acts[i] = makeActivity(event, tool, input, ref.Add(-time.Duration(n-i)*spacing), "s1")
	}
	return acts
}

// compileTestRule pre-compiles regex patterns on a rule, mirroring what
// ParseRuleFile does at load time. Tests construct rules directly and must
// call this to populate the compiled fields.
func compileTestRule(r *types.Rule) {
	for j := range r.Trigger.Conditions {
		c := &r.Trigger.Conditions[j]
		if c.Tool != "" {
			c.ToolRe = regexp.MustCompile("^(?:" + c.Tool + ")$")
		}
		if c.DiffPattern != "" {
			c.DiffPatternRe = regexp.MustCompile(c.DiffPattern)
		}
	}
	if r.Action.ToolScope != "" {
		r.Action.ToolScopeRe = regexp.MustCompile("^(?:" + r.Action.ToolScope + ")$")
	}
}

// makeRule creates an enabled rule with "and" logic and ActionLog default.
// It also compiles any regex patterns so the rule is ready for evaluation.
func makeRule(name string, conds []types.Condition, opts ...func(*types.Rule)) types.Rule {
	r := types.Rule{
		Name:    name,
		Enabled: true,
		Trigger: types.Trigger{Logic: "and", Conditions: conds},
		Action:  types.Action{Type: types.ActionLog},
	}
	for _, opt := range opts {
		opt(&r)
	}
	compileTestRule(&r)
	return r
}

func withAction(at types.ActionType, msg string) func(*types.Rule) {
	return func(r *types.Rule) { r.Action.Type = at; r.Action.Message = msg }
}

func withCooldown(cd string) func(*types.Rule) {
	return func(r *types.Rule) { r.Action.Cooldown = cd }
}

func withLogic(logic string) func(*types.Rule) {
	return func(r *types.Rule) { r.Trigger.Logic = logic }
}

func withPriority(p int) func(*types.Rule) {
	return func(r *types.Rule) { r.Priority = p }
}

func withDisabled() func(*types.Rule) {
	return func(r *types.Rule) { r.Enabled = false }
}

// scenario is a table-driven test case for rule evaluation.
type scenario struct {
	name       string
	activities []types.Activity
	wantMatch  int
	wantAction types.ActionType // checked only when non-empty and wantMatch > 0
}

// runScenarios runs table-driven rule evaluation tests.
func runScenarios(t *testing.T, rule types.Rule, now time.Time, tests []scenario) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine([]types.Rule{rule})
			matches := engine.Evaluate(tt.activities, types.Event{Timestamp: now})
			if len(matches) != tt.wantMatch {
				t.Fatalf("expected %d matches, got %d", tt.wantMatch, len(matches))
			}
			if tt.wantAction != "" && tt.wantMatch > 0 && matches[0].Rule.Action.Type != tt.wantAction {
				t.Errorf("expected action %q, got %q", tt.wantAction, matches[0].Rule.Action.Type)
			}
		})
	}
}

// loadTestRules loads default rules from the project rules/ directory.
func loadTestRules(t *testing.T) []types.Rule {
	t.Helper()
	rules, err := LoadRules(filepath.Join("..", "..", "rules"))
	if err != nil {
		t.Fatalf("failed to load default rules: %v", err)
	}
	return rules
}

func findRule(t *testing.T, rules []types.Rule, name string) types.Rule {
	t.Helper()
	for _, r := range rules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("rule %q not found in default rules", name)
	return types.Rule{}
}

// --- YAML Parsing Tests ---

func TestParseRuleFile(t *testing.T) {
	dir := t.TempDir()
	content := `rules:
  - name: test-rule
    description: "A test rule"
    enabled: true
    priority: 5
    trigger:
      logic: and
      conditions:
        - event: PostToolUse
          tool: "Edit"
          count: 2
          within: "5m"
    action:
      type: block
      message: "blocked"
`
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	rules, err := ParseRuleFile(path)
	if err != nil {
		t.Fatalf("ParseRuleFile() error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}

	r := rules[0]
	if r.Name != "test-rule" {
		t.Errorf("name = %q, want %q", r.Name, "test-rule")
	}
	if !r.Enabled {
		t.Error("expected rule to be enabled")
	}
	if r.Priority != 5 {
		t.Errorf("priority = %d, want 5", r.Priority)
	}
	if r.Trigger.Logic != "and" {
		t.Errorf("logic = %q, want %q", r.Trigger.Logic, "and")
	}
	if len(r.Trigger.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(r.Trigger.Conditions))
	}
	cond := r.Trigger.Conditions[0]
	if cond.Event != "PostToolUse" {
		t.Errorf("event = %q, want %q", cond.Event, "PostToolUse")
	}
	if cond.Tool != "Edit" {
		t.Errorf("tool = %q, want %q", cond.Tool, "Edit")
	}
	if cond.Count != 2 {
		t.Errorf("count = %d, want 2", cond.Count)
	}
	if cond.Within != "5m" {
		t.Errorf("within = %q, want %q", cond.Within, "5m")
	}
	if r.Action.Type != types.ActionBlock {
		t.Errorf("action type = %q, want %q", r.Action.Type, types.ActionBlock)
	}
}

func TestParseRuleFile_DefaultLogic(t *testing.T) {
	dir := t.TempDir()
	content := `rules:
  - name: no-logic
    enabled: true
    trigger:
      conditions:
        - event: PreToolUse
    action:
      type: log
      message: "logged"
`
	path := filepath.Join(dir, "nologic.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	rules, err := ParseRuleFile(path)
	if err != nil {
		t.Fatalf("ParseRuleFile() error: %v", err)
	}
	if rules[0].Trigger.Logic != "and" {
		t.Errorf("expected default logic 'and', got %q", rules[0].Trigger.Logic)
	}
}

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()

	yaml1 := `rules:
  - name: rule-a
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
    action:
      type: log
      message: "a"
`
	yaml2 := `rules:
  - name: rule-b
    enabled: true
    trigger:
      conditions:
        - event: PreToolUse
    action:
      type: log
      message: "b"
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(yaml1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(yaml2), 0644); err != nil {
		t.Fatal(err)
	}
	// Non-yaml files should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	rules, err := LoadRules(dir)
	if err != nil {
		t.Fatalf("LoadRules() error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestLoadRules_NonexistentDir(t *testing.T) {
	_, err := LoadRules("/nonexistent/path/squawk/rules")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestLoadDefaultRules(t *testing.T) {
	rules := loadTestRules(t)
	if len(rules) == 0 {
		t.Fatal("expected at least one default rule")
	}
	found := false
	for _, r := range rules {
		if r.Name == "test-only-modification" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'test-only-modification' rule in defaults")
	}
}

// --- Engine Evaluation Tests ---

func TestEvaluateEventMatch(t *testing.T) {
	now := time.Now()
	rule := makeRule("event-match", []types.Condition{{Event: "PostToolUse"}}, withAction(types.ActionLog, "matched"))
	engine := NewEngine([]types.Rule{rule})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{HookEventName: "PostToolUse", Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "event-match" {
		t.Errorf("matched rule = %q, want %q", matches[0].Rule.Name, "event-match")
	}
}

func TestEvaluateToolRegex(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name, toolRegex, toolName string
		wantMatch                 bool
	}{
		{"exact match", "Edit", "Edit", true},
		{"regex or", "Edit|Write", "Write", true},
		{"regex or no match", "Edit|Write", "Read", false},
		{"partial no match", "Edit", "EditFoo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := makeRule("tool-test", []types.Condition{{Tool: tt.toolRegex}})
			engine := NewEngine([]types.Rule{rule})
			activities := []types.Activity{
				makeActivity("PostToolUse", tt.toolName, nil, now.Add(-1*time.Minute), "s1"),
			}
			matches := engine.Evaluate(activities, types.Event{Timestamp: now})
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateFilePattern(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name, pattern, filePath string
		wantMatch               bool
	}{
		{"go test file", "*_test.go", "/foo/bar/service_test.go", true},
		{"ts test file", "*.test.ts", "/src/utils.test.ts", true},
		{"not test file", "*_test.go", "/foo/bar/service.go", false},
		{"pipe-separated patterns", "*_test.go|*.test.ts", "/src/thing.test.ts", true},
		{"no file_path in input", "*_test.go", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := makeRule("file-test", []types.Condition{{FilePattern: tt.pattern}})
			engine := NewEngine([]types.Rule{rule})
			var input map[string]any
			if tt.filePath != "" {
				input = fileInput(tt.filePath)
			}
			activities := []types.Activity{
				makeActivity("PostToolUse", "Edit", input, now.Add(-1*time.Minute), "s1"),
			}
			matches := engine.Evaluate(activities, types.Event{Timestamp: now})
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateCountThreshold(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		count     int
		numEvents int
		wantMatch bool
	}{
		{"exactly at threshold", 3, 3, true},
		{"above threshold", 3, 5, true},
		{"below threshold", 3, 2, false},
		{"zero count defaults to 1", 0, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := makeRule("count-test", []types.Condition{{Event: "PostToolUse", Count: tt.count}})
			engine := NewEngine([]types.Rule{rule})
			var activities []types.Activity
			for i := 0; i < tt.numEvents; i++ {
				activities = append(activities, makeActivity("PostToolUse", "Edit", nil, now.Add(-time.Duration(i)*time.Second), "s1"))
			}
			matches := engine.Evaluate(activities, types.Event{Timestamp: now})
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateTimeWindow(t *testing.T) {
	now := time.Now()
	rule := makeRule("window-test", []types.Condition{{Event: "PostToolUse", Count: 2, Within: "5m"}})
	runScenarios(t, rule, now, []scenario{
		{
			name: "within window",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "outside window",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-10*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestEvaluateNegate(t *testing.T) {
	now := time.Now()
	rule := makeRule("negate-test", []types.Condition{
		{Event: "PostToolUse", Tool: "Read", Negate: true, Count: 1, Within: "5m"},
	})
	runScenarios(t, rule, now, []scenario{
		{
			name: "passes when no matching activities",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "fails when matching activities exist",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Read", nil, now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestEvaluateAndLogic(t *testing.T) {
	now := time.Now()
	rule := makeRule("and-logic", []types.Condition{
		{Event: "PostToolUse", Tool: "Edit", Count: 2},
		{Event: "PostToolUse", Tool: "Bash", Count: 1},
	})
	runScenarios(t, rule, now, []scenario{
		{
			name: "both conditions met",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Bash", nil, now.Add(-3*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "only first condition met",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestEvaluateOrLogic(t *testing.T) {
	now := time.Now()
	rule := makeRule("or-logic", []types.Condition{
		{Event: "PostToolUse", Tool: "Edit", Count: 3},
		{Event: "PostToolUse", Tool: "Write", Count: 1},
	}, withLogic("or"))
	runScenarios(t, rule, now, []scenario{
		{
			name: "first condition met only",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", nil, now.Add(-3*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "second condition met only",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Write", nil, now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "neither condition met",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Read", nil, now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestEvaluateDisabledRule(t *testing.T) {
	now := time.Now()
	rule := makeRule("disabled-rule", []types.Condition{{Event: "PostToolUse"}}, withDisabled())
	engine := NewEngine([]types.Rule{rule})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for disabled rule, got %d", len(matches))
	}
}

func TestEvaluatePrioritySort(t *testing.T) {
	now := time.Now()
	rules := []types.Rule{
		makeRule("low-priority", []types.Condition{{Event: "PostToolUse"}}, withPriority(1)),
		makeRule("high-priority", []types.Condition{{Event: "PostToolUse"}}, withPriority(10)),
		makeRule("mid-priority", []types.Condition{{Event: "PostToolUse"}}, withPriority(5)),
	}
	engine := NewEngine(rules)
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	if matches[0].Rule.Priority != 10 {
		t.Errorf("first match priority = %d, want 10", matches[0].Rule.Priority)
	}
	if matches[1].Rule.Priority != 5 {
		t.Errorf("second match priority = %d, want 5", matches[1].Rule.Priority)
	}
	if matches[2].Rule.Priority != 1 {
		t.Errorf("third match priority = %d, want 1", matches[2].Rule.Priority)
	}
}

// --- FilePatternExclude Tests ---

func TestEvaluateFilePatternExclude(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name, exclude, filePath string
		wantMatch               bool
	}{
		{"source file not excluded", "*_test.go", "/foo/handler.go", true},
		{"test file excluded", "*_test.go", "/foo/handler_test.go", false},
		{"no file_path treated as not excluded", "*_test.go", "", true},
		{"pipe-separated exclude", "*_test.go|*.test.ts", "/foo/handler.go", true},
		{"pipe-separated exclude matches", "*_test.go|*.test.ts", "/foo/app.test.ts", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := makeRule("exclude-test", []types.Condition{
				{Event: "PostToolUse", FilePatternExclude: tt.exclude},
			})
			engine := NewEngine([]types.Rule{rule})
			var input map[string]any
			if tt.filePath != "" {
				input = fileInput(tt.filePath)
			}
			activities := []types.Activity{
				makeActivity("PostToolUse", "Edit", input, now.Add(-1*time.Minute), "s1"),
			}
			matches := engine.Evaluate(activities, types.Event{Timestamp: now})
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

// --- Scenario Tests: test-only-modification ---

func newTestOnlyModificationRule() types.Rule {
	sourceOfIdx := 0
	r := types.Rule{
		Name:     "test-only-modification",
		Enabled:  true,
		Priority: 10,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{
					Event:       "PostToolUse",
					Tool:        "Edit|Write",
					FilePattern: "*_test.go|*.test.ts",
					Count:       3,
					Within:      "5m",
				},
				{
					Event:    "PostToolUse",
					Tool:     "Read|Glob|Grep",
					SourceOf: &sourceOfIdx,
					Negate:   true,
					Count:    1,
					Within:   "5m",
				},
				{
					Event:              "PostToolUse",
					Tool:               "Edit|Write",
					FilePatternExclude: "*_test.go|*.test.ts",
					Negate:             true,
					Count:              1,
					Within:             "5m",
				},
			},
		},
		Action: types.Action{
			Type:    types.ActionBlock,
			Message: "Stop modifying tests blindly.",
		},
	}
	compileTestRule(&r)
	return r
}

func TestScenarios_TestOnlyModification(t *testing.T) {
	now := time.Now()
	rule := newTestOnlyModificationRule()

	// Helper: 2 test-file edits, one intervening tool, 1 more test-file edit.
	withReset := func(name, tool string, input map[string]any) scenario {
		return scenario{
			name: name,
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", tool, input, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		}
	}

	runScenarios(t, rule, now, []scenario{
		{
			name: "pure test edits blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		withReset("read source resets allowed", "Read", fileInput("/p/handler.go")),
		withReset("edit source resets allowed", "Edit", fileInput("/p/handler.go")),
		withReset("grep source dir resets allowed", "Grep", map[string]any{"pattern": "func", "path": "/p"}),
		{
			name: "bash does not reset still blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Bash", nil, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "below threshold allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "outside time window allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-10*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-8*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-7*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "non-test file edits allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/main.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "source edit within window resets counter",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-5*time.Minute+10*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
	})
}

// --- Cooldown Tests ---

func TestCooldown_MatchThenSuppressThenMatchAgain(t *testing.T) {
	now := time.Now()
	rule := makeRule("cooldown-rule", []types.Condition{
		{Event: "PostToolUse", Tool: "Bash", Count: 2, Within: "5m"},
	}, withAction(types.ActionBlock, "blocked"), withCooldown("30s"))
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation (PreToolUse): should match and set cooldown.
	matches := engine.Evaluate(activities, types.Event{HookEventName: "PreToolUse", Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("first eval: expected 1 match, got %d", len(matches))
	}

	// Second evaluation 10s later: within cooldown, should NOT match.
	activities = append(activities, makeActivity("PostToolUse", "Bash", nil, now.Add(5*time.Second), "s1"))
	matches = engine.Evaluate(activities, types.Event{HookEventName: "PreToolUse", Timestamp: now.Add(10 * time.Second)})
	if len(matches) != 0 {
		t.Fatalf("during cooldown: expected 0 matches, got %d", len(matches))
	}

	// Third evaluation 31s later: cooldown expired, should match again.
	activities = append(activities, makeActivity("PostToolUse", "Bash", nil, now.Add(30*time.Second), "s1"))
	matches = engine.Evaluate(activities, types.Event{HookEventName: "PreToolUse", Timestamp: now.Add(31 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("after cooldown: expected 1 match, got %d", len(matches))
	}
}

func TestCooldown_DoesNotAffectOtherRules(t *testing.T) {
	now := time.Now()
	ruleA := makeRule("rule-a", []types.Condition{
		{Event: "PostToolUse", Tool: "Bash"},
	}, withAction(types.ActionBlock, "blocked-a"), withCooldown("1m"))
	ruleB := makeRule("rule-b", []types.Condition{
		{Event: "PostToolUse", Tool: "Bash"},
	}) // No cooldown.
	engine := NewEngine([]types.Rule{ruleA, ruleB})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation (PreToolUse so block cooldown is set): both should match.
	matches := engine.Evaluate(activities, types.Event{HookEventName: "PreToolUse", Timestamp: now})
	if len(matches) != 2 {
		t.Fatalf("first eval: expected 2 matches, got %d", len(matches))
	}

	// Second evaluation 10s later: rule-a in cooldown, rule-b should still match.
	matches = engine.Evaluate(activities, types.Event{HookEventName: "PreToolUse", Timestamp: now.Add(10 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("during cooldown: expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "rule-b" {
		t.Errorf("expected rule-b to match, got %q", matches[0].Rule.Name)
	}
}

func TestCooldown_NoCooldown_AlwaysMatches(t *testing.T) {
	now := time.Now()
	rule := makeRule("no-cooldown", []types.Condition{{Event: "PostToolUse", Tool: "Edit"}})
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}

	for i := 0; i < 5; i++ {
		ts := now.Add(time.Duration(i) * time.Second)
		matches := engine.Evaluate(activities, types.Event{Timestamp: ts})
		if len(matches) != 1 {
			t.Fatalf("eval %d: expected 1 match, got %d", i, len(matches))
		}
	}
}

func TestCooldown_ExactExpiry(t *testing.T) {
	now := time.Now()
	rule := makeRule("exact-expiry", []types.Condition{
		{Event: "PostToolUse", Tool: "Bash"},
	}, withAction(types.ActionBlock, "blocked"), withCooldown("30s"))
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation: should match, sets cooldown expiry to now+30s.
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("first eval: expected 1 match, got %d", len(matches))
	}

	// At exactly the cooldown expiry: now+30s is NOT before expiry, so should match.
	matches = engine.Evaluate(activities, types.Event{Timestamp: now.Add(30 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("at exact expiry: expected 1 match, got %d", len(matches))
	}
}

func TestCooldown_BlockSkipsCooldownOnPostToolUse(t *testing.T) {
	now := time.Now()
	rule := makeRule("block-cooldown", []types.Condition{
		{Event: "PostToolUse", Tool: "Edit", Count: 3, Within: "5m"},
	}, withAction(types.ActionBlock, "blocked"), withCooldown("30s"))
	engine := NewEngine([]types.Rule{rule})

	activities := nActivities(3, "PostToolUse", "Edit", fileInput("/a_test.go"), time.Second, now)

	// Evaluate with a PostToolUse event: rule matches but cooldown should NOT
	// be set because block actions can't execute on PostToolUse.
	matches := engine.Evaluate(activities, types.Event{
		HookEventName: "PostToolUse",
		Timestamp:     now,
	})
	if len(matches) != 1 {
		t.Fatalf("PostToolUse eval: expected 1 match, got %d", len(matches))
	}

	// Immediately evaluate with a PreToolUse event: should still match because
	// no cooldown was set by the PostToolUse evaluation.
	matches = engine.Evaluate(activities, types.Event{
		HookEventName: "PreToolUse",
		Timestamp:     now.Add(1 * time.Second),
	})
	if len(matches) != 1 {
		t.Fatalf("PreToolUse eval: expected 1 match (no cooldown), got %d", len(matches))
	}

	// Now cooldown IS set (from the PreToolUse). Next PreToolUse within 30s
	// should be suppressed.
	matches = engine.Evaluate(activities, types.Event{
		HookEventName: "PreToolUse",
		Timestamp:     now.Add(10 * time.Second),
	})
	if len(matches) != 0 {
		t.Fatalf("during cooldown: expected 0 matches, got %d", len(matches))
	}
}

// --- matchFilePattern Tests ---

func TestMatchFilePattern(t *testing.T) {
	tests := []struct {
		name, pattern, path string
		want                bool
	}{
		{"go test match", "*_test.go", "/a/b/foo_test.go", true},
		{"go test no match", "*_test.go", "/a/b/foo.go", false},
		{"ts test match", "*.test.ts", "/src/bar.test.ts", true},
		{"pipe-separated first", "*_test.go|*.test.ts", "/a/b_test.go", true},
		{"pipe-separated second", "*_test.go|*.test.ts", "/a/b.test.ts", true},
		{"pipe-separated none", "*_test.go|*.test.ts", "/a/b.go", false},
		{"spec file", "*.spec.js", "/src/app.spec.js", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchFilePattern(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchFilePattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

// --- Edge Cases ---

func TestEvaluateNoRules(t *testing.T) {
	engine := NewEngine(nil)
	matches := engine.Evaluate(nil, types.Event{Timestamp: time.Now()})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches with no rules, got %d", len(matches))
	}
}

func TestEvaluateNoActivities(t *testing.T) {
	rule := makeRule("needs-activities", []types.Condition{{Event: "PostToolUse", Count: 1}})
	engine := NewEngine([]types.Rule{rule})
	matches := engine.Evaluate(nil, types.Event{Timestamp: time.Now()})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches with no activities, got %d", len(matches))
	}
}

// =============================================================================
// Default Rule Scenario Tests
// =============================================================================

func TestScenarios_BlindFileCreation(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "blind-file-creation")
	now := time.Now()

	threeWrites := []types.Activity{
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-1*time.Minute), "s1"),
	}

	// Helper: explorer tool before 3 writes should not trigger.
	afterExplore := func(name, tool string, input map[string]any) scenario {
		return scenario{
			name:       name,
			activities: append([]types.Activity{makeActivity("PostToolUse", tool, input, now.Add(-4*time.Minute), "s1")}, threeWrites...),
			wantMatch:  0,
		}
	}

	runScenarios(t, rule, now, []scenario{
		{
			name:       "3 writes no reads triggered",
			activities: threeWrites,
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		afterExplore("writes after read allowed", "Read", fileInput("/p/existing.go")),
		afterExplore("writes after glob allowed", "Glob", nil),
		afterExplore("writes after grep allowed", "Grep", nil),
		{
			name: "2 writes allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name:       "outside window allowed",
			activities: nActivities(3, "PostToolUse", "Write", fileInput("/p/new.go"), time.Minute, now.Add(-5*time.Minute)),
			wantMatch:  0,
		},
		{
			name: "edits dont count allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/file1.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/file2.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/file3.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_ExcessiveEdits(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "same-file-excessive-edits")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name:       "8 edits triggered",
			activities: nActivities(8, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now),
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name:       "7 edits allowed",
			activities: nActivities(7, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now),
			wantMatch:  0,
		},
		{
			name:       "outside window allowed",
			activities: nActivities(8, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now.Add(-6*time.Minute)),
			wantMatch:  0,
		},
		{
			name: "spread across files allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-210*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/other.go"), now.Add(-150*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/config.go"), now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0, // 5 edits on handler.go, 1 each on 3 others — no single file reaches 8
		},
		{
			name: "8 same file with other files triggered",
			activities: append(
				nActivities(8, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now),
				makeActivity("PostToolUse", "Write", fileInput("/p/other.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/config.go"), now.Add(-1*time.Minute), "s1"),
			),
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name:       "10 edits triggered",
			activities: nActivities(10, "PostToolUse", "Edit", fileInput("/p/handler.go"), 20*time.Second, now),
			wantMatch:  1,
		},
		{
			name: "reads dont count allowed",
			activities: append(
				nActivities(7, "PostToolUse", "Edit", fileInput("/p/handler.go"), 20*time.Second, now),
				nActivities(3, "PostToolUse", "Read", fileInput("/p/handler.go"), 15*time.Second, now)...,
			),
			wantMatch: 0,
		},
	})
}

func TestScenarios_WriteBeforeRead(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "write-before-read")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "3 edits no reads triggered",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name: "writes after grep allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Grep", nil, now.Add(-100*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "writes after read allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Read", fileInput("/p/existing.go"), now.Add(-100*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "outside window allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-5*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-3*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "2 edits allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "mixed edit write triggered",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "glob resets allowed",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-100*time.Second), "s1"),
				makeActivity("PostToolUse", "Glob", nil, now.Add(-80*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
	})
}

// =============================================================================
// Hash/Diff Helper Unit Tests
// =============================================================================

func TestContentHash_Determinism(t *testing.T) {
	act := makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), time.Now(), "s1")
	h1 := contentHash(act)
	h2 := contentHash(act)
	if h1 != h2 {
		t.Errorf("contentHash not deterministic: %d != %d", h1, h2)
	}
}

func TestContentHash_DifferentPaths(t *testing.T) {
	act1 := makeActivity("PostToolUse", "Edit", editInput("/p/a.go", "", "body"), time.Now(), "s1")
	act2 := makeActivity("PostToolUse", "Edit", editInput("/p/b.go", "", "body"), time.Now(), "s1")
	if contentHash(act1) == contentHash(act2) {
		t.Error("contentHash should differ for different file paths")
	}
}

func TestEditHash_Determinism(t *testing.T) {
	act := makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), time.Now(), "s1")
	h1 := editHash(act)
	h2 := editHash(act)
	if h1 != h2 {
		t.Errorf("editHash not deterministic: %d != %d", h1, h2)
	}
}

func TestEditHash_DifferentEdits(t *testing.T) {
	act1 := makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old1", "new1"), time.Now(), "s1")
	act2 := makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old2", "new2"), time.Now(), "s1")
	if editHash(act1) == editHash(act2) {
		t.Error("editHash should differ for different old/new strings")
	}
}

func TestCommandHash_Determinism(t *testing.T) {
	act := makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), time.Now(), "s1")
	h1 := commandHash(act)
	h2 := commandHash(act)
	if h1 != h2 {
		t.Errorf("commandHash not deterministic: %d != %d", h1, h2)
	}
}

func TestCommandHash_DifferentCommands(t *testing.T) {
	act1 := makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), time.Now(), "s1")
	act2 := makeActivity("PostToolUseFailure", "Bash", bashInput("go build ./..."), time.Now(), "s1")
	if commandHash(act1) == commandHash(act2) {
		t.Error("commandHash should differ for different commands")
	}
}

func TestDiffFilterActivities_PatternMatch(t *testing.T) {
	now := time.Now()
	acts := []types.Activity{
		// old has assert, new does not → removal
		makeActivity("PostToolUse", "Edit", editInput("/p/foo_test.go", "assert.Equal(t, 1, 1)", "// removed"), now, "s1"),
		// old has assert, new also has assert → no removal
		makeActivity("PostToolUse", "Edit", editInput("/p/bar_test.go", "assert.Equal(t, 1, 1)", "assert.Equal(t, 2, 2)"), now, "s1"),
	}
	result := diffFilterActivities(regexp.MustCompile("assert"), 0, acts)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestDiffFilterActivities_ShrinkRatio(t *testing.T) {
	now := time.Now()
	acts := []types.Activity{
		// old=100 chars, new=40 chars → 40 < 0.5*100 → shrink detected
		makeActivity("PostToolUse", "Edit", editInput("/p/foo.go",
			strings.Repeat("x", 100), strings.Repeat("y", 40)), now, "s1"),
		// old=100 chars, new=60 chars → 60 >= 0.5*100 → no shrink
		makeActivity("PostToolUse", "Edit", editInput("/p/bar.go",
			strings.Repeat("x", 100), strings.Repeat("y", 60)), now, "s1"),
		// old is empty → skip
		makeActivity("PostToolUse", "Edit", editInput("/p/baz.go",
			"", strings.Repeat("y", 10)), now, "s1"),
	}
	// Pass nil for patternRe since we only test shrink ratio here (no regex pattern).
	var noPattern *regexp.Regexp
	result := diffFilterActivities(noPattern, 0.5, acts)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

// =============================================================================
// Default Rule Scenario Tests — Hash/Diff Rules
// =============================================================================

func TestScenarios_EditOscillation(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "edit-oscillation")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "A→B→A triggers",
			activities: []types.Activity{
				// State A
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateA"), now.Add(-3*time.Minute), "s1"),
				// State B
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateB"), now.Add(-2*time.Minute), "s1"),
				// Back to State A
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateA"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionBlock,
		},
		{
			name: "A→B→C does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateA"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateB"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateC"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "different files dont cross-trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/a.go", "", "stateA"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/b.go", "", "stateB"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/a.go", "", "stateB"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "outside window does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateA"), now.Add(-15*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateB"), now.Add(-12*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "", "stateA"), now.Add(-11*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_RepeatedIdenticalEdit(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "repeated-identical-edit")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "same edit 3x triggers",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionBlock,
		},
		{
			name: "same edit 2x does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old", "new"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "different edits dont trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old1", "new1"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old2", "new2"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo.go", "old3", "new3"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_RepeatedFailingCommand(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "repeated-failing-command")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "same cmd 3x failure triggers",
			activities: []types.Activity{
				makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), now.Add(-30*time.Second), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionBlock,
		},
		{
			name: "different cmds dont trigger",
			activities: []types.Activity{
				makeActivity("PostToolUseFailure", "Bash", bashInput("go test ./..."), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUseFailure", "Bash", bashInput("go build ./..."), now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUseFailure", "Bash", bashInput("go vet ./..."), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "success events not counted",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Bash", bashInput("go test ./..."), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Bash", bashInput("go test ./..."), now.Add(-1*time.Minute), "s1"),
				makeActivity("PostToolUse", "Bash", bashInput("go test ./..."), now.Add(-30*time.Second), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_WholeFileRewrite(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "whole-file-rewrite")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "write after read triggers",
			activities: []types.Activity{
				// Read the file first (establishes it as known).
				makeActivity("PostToolUse", "Read", fileInput("/p/handler.go"), now.Add(-4*time.Minute), "s1"),
				// Then rewrite it twice with Write.
				makeActivity("PostToolUse", "Write", writeInput("/p/handler.go", "rewrite1"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", writeInput("/p/handler.go", "rewrite2"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name: "write new file does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Write", writeInput("/p/brand_new.go", "content1"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", writeInput("/p/also_new.go", "content2"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "write after edit triggers",
			activities: []types.Activity{
				// Edit establishes file as known.
				makeActivity("PostToolUse", "Edit", editInput("/p/service.go", "old", "new"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", writeInput("/p/service.go", "full rewrite 1"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Write", writeInput("/p/service.go", "full rewrite 2"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
	})
}

func TestScenarios_TestAssertionWeakening(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "test-assertion-weakening")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "removing assert triggers",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo_test.go",
					"assert.Equal(t, want, got)", "// simplified"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo_test.go",
					"require.NoError(t, err)", "// removed check"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionBlock,
		},
		{
			name: "adding assert does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/foo_test.go",
					"// placeholder", "assert.Equal(t, want, got)"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/foo_test.go",
					"// placeholder2", "require.NoError(t, err)"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "non-test file does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/handler.go",
					"assert.Equal(t, want, got)", "// removed"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/handler.go",
					"require.NoError(t, err)", "// removed"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_ErrorHandlingRemoval(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "error-handling-removal")
	now := time.Now()
	runScenarios(t, rule, now, []scenario{
		{
			name: "removing error handling triggers",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/handler.go",
					"if err != nil { return err }", "// just continue"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/service.go",
					"if err != nil { log.Fatal(err) }", "// ignore"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionBlock,
		},
		{
			name: "adding error handling does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/handler.go",
					"result := doSomething()", "if err != nil { return err }"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/service.go",
					"val := process()", "if err != nil { log.Fatal(err) }"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_LargeCodeDeletion(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "large-code-deletion")
	now := time.Now()
	large := strings.Repeat("x", 200)
	small := strings.Repeat("y", 80) // 80 < 0.5*200 → shrink
	runScenarios(t, rule, now, []scenario{
		{
			name: "3x shrink >50% triggers",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/a.go", large, small), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/b.go", large, small), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/c.go", large, small), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name: "shrink <50% does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/a.go", large, strings.Repeat("y", 120)), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/b.go", large, strings.Repeat("y", 120)), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/c.go", large, strings.Repeat("y", 120)), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "2x shrink below threshold does not trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", editInput("/p/a.go", large, small), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", editInput("/p/b.go", large, small), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestScenarios_SessionContext(t *testing.T) {
	rule := findRule(t, loadTestRules(t), "session-context-warning")
	now := time.Now()

	// Pre-build complex activity sets.
	mixedToolActs := func() []types.Activity {
		tools := []string{"Edit", "Write", "Read", "Bash", "Grep", "Glob"}
		acts := make([]types.Activity, 50)
		for i := range acts {
			acts[i] = makeActivity("PostToolUse", tools[i%len(tools)], nil,
				now.Add(-time.Duration(50-i)*30*time.Second), "s1")
		}
		return acts
	}()

	splitWindowActs := func() []types.Activity {
		var acts []types.Activity
		for i := 0; i < 25; i++ {
			acts = append(acts, makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"),
				now.Add(-35*time.Minute-time.Duration(i)*time.Minute), "s1"))
		}
		for i := 0; i < 30; i++ {
			acts = append(acts, makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"),
				now.Add(-time.Duration(30-i)*time.Minute+30*time.Second), "s1"))
		}
		return acts
	}()

	preToolUseActs := func() []types.Activity {
		var acts []types.Activity
		for i := 0; i < 30; i++ {
			acts = append(acts, makeActivity("PostToolUse", "Edit", nil,
				now.Add(-time.Duration(50-i)*30*time.Second), "s1"))
		}
		for i := 0; i < 25; i++ {
			acts = append(acts, makeActivity("PreToolUse", "Edit", nil,
				now.Add(-time.Duration(25-i)*30*time.Second), "s1"))
		}
		return acts
	}()

	runScenarios(t, rule, now, []scenario{
		{
			name:       "50 events triggered",
			activities: nActivities(50, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now),
			wantMatch:  1,
			wantAction: types.ActionInject,
		},
		{
			name:       "49 events allowed",
			activities: nActivities(49, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now),
			wantMatch:  0,
		},
		{
			name:       "outside window allowed",
			activities: nActivities(50, "PostToolUse", "Edit", fileInput("/p/handler.go"), 30*time.Second, now.Add(-31*time.Minute)),
			wantMatch:  0,
		},
		{
			name:       "mixed tools triggered",
			activities: mixedToolActs,
			wantMatch:  1,
		},
		{
			name:       "split across window only 30 counted",
			activities: splitWindowActs,
			wantMatch:  0,
		},
		{
			name:       "preToolUse not counted",
			activities: preToolUseActs,
			wantMatch:  0,
		},
	})
}

// --- source_of Tests ---

func TestTestToSourceFile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Go
		{"/p/calc_test.go", "/p/calc.go"},
		{"/p/handler_test.go", "/p/handler.go"},
		{"calc_test.go", "calc.go"},

		// JS/TS .test.
		{"/src/calc.test.ts", "/src/calc.ts"},
		{"/src/App.test.tsx", "/src/App.tsx"},
		{"/src/utils.test.js", "/src/utils.js"},

		// JS/TS .spec.
		{"/src/calc.spec.ts", "/src/calc.ts"},
		{"/src/App.spec.tsx", "/src/App.tsx"},

		// Python test_ prefix
		{"/tests/test_calc.py", "/tests/calc.py"},

		// Python _test suffix
		{"/tests/calc_test.py", "/tests/calc.py"},

		// No match
		{"/p/calc.go", ""},
		{"/p/README.md", ""},
		{"/p/main.go", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := testToSourceFile(tt.input)
			if got != tt.want {
				t.Errorf("testToSourceFile(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSourceOf_ReadRelatedSource(t *testing.T) {
	now := time.Now()
	rule := newTestOnlyModificationRule()

	runScenarios(t, rule, now, []scenario{
		{
			name: "read related source file allows",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Read", fileInput("/p/calc.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "read unrelated source file still blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Read", fileInput("/p/utils.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "read test file itself still blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Read", fileInput("/p/calc_test.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "glob without path still blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Glob", nil, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "grep in source directory allows",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Grep", map[string]any{"pattern": "func Add", "path": "/p"}, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "grep in different directory still blocked",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Grep", map[string]any{"pattern": "func Add", "path": "/other"}, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "multiple test files read one source allows",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Read", fileInput("/p/calc.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/calc_test.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "JS test file read related source allows",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/src/App.test.ts"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/src/App.test.ts"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Read", fileInput("/src/App.ts"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/src/App.test.ts"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	})
}

func TestSourceOf_ParserValidation(t *testing.T) {
	// source_of referencing a future condition should fail validation.
	dir := t.TempDir()
	yaml := `rules:
  - name: bad-source-of
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
          tool: "Read"
          source_of: 1
          negate: true
          count: 1
          within: "5m"
        - event: PostToolUse
          tool: "Edit"
          file_pattern: "*_test.go"
          count: 3
          within: "5m"
    action:
      type: block
      message: "test"
`
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRules(dir)
	if err == nil {
		t.Fatal("expected error for source_of referencing non-previous condition")
	}
	if !strings.Contains(err.Error(), "source_of") {
		t.Fatalf("error should mention source_of, got: %v", err)
	}
}

func TestGroupByFile(t *testing.T) {
	now := time.Now()
	rule := types.Rule{
		Name:    "per-file-count",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse", Tool: "Edit|Write", Count: 3, Within: "5m", GroupBy: "file"},
			},
		},
		Action: types.Action{Type: types.ActionInject, Message: "too many edits"},
	}
	compileTestRule(&rule)

	tests := []struct {
		name       string
		activities []types.Activity
		wantMatch  int
	}{
		{
			name: "3 edits same file triggers",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name: "3 edits different files no trigger",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/b.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/c.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
		{
			name: "one file reaches threshold among many",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-4*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/b.go"), now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/b.go"), now.Add(-90*time.Second), "s1"),
				makeActivity("PostToolUse", "Edit", fileInput("/p/a.go"), now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 1,
		},
		{
			name:       "no activities no trigger",
			activities: nil,
			wantMatch:  0,
		},
		{
			name: "activities without file_path ignored",
			activities: []types.Activity{
				makeActivity("PostToolUse", "Edit", map[string]any{}, now.Add(-3*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", map[string]any{}, now.Add(-2*time.Minute), "s1"),
				makeActivity("PostToolUse", "Edit", map[string]any{}, now.Add(-1*time.Minute), "s1"),
			},
			wantMatch: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine([]types.Rule{rule})
			event := types.Event{HookEventName: "PostToolUse", ToolName: "Edit", Timestamp: now}
			matches := engine.Evaluate(tt.activities, event)
			if len(matches) != tt.wantMatch {
				t.Errorf("got %d matches, want %d", len(matches), tt.wantMatch)
			}
		})
	}
}

func TestGroupBy_ParserValidation(t *testing.T) {
	dir := t.TempDir()
	yaml := `rules:
  - name: bad-group-by
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
          tool: "Edit"
          count: 3
          within: "5m"
          group_by: "session"
    action:
      type: inject
      message: "test"
`
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRules(dir)
	if err == nil {
		t.Fatal("expected error for invalid group_by value")
	}
	if !strings.Contains(err.Error(), "group_by") {
		t.Fatalf("error should mention group_by, got: %v", err)
	}
}
