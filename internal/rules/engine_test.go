package rules

import (
	"os"
	"path/filepath"
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
	// Locate the default rules relative to the test file.
	// This ensures our parser works with the actual shipped rules.
	projectRoot := filepath.Join("..", "..", "rules")
	rules, err := LoadRules(projectRoot)
	if err != nil {
		t.Fatalf("failed to load default rules: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("expected at least one default rule")
	}
	// Verify the flagship rule exists.
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
	rule := types.Rule{
		Name:    "event-match",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse"},
			},
		},
		Action: types.Action{Type: types.ActionLog, Message: "matched"},
	}
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	currentEvent := types.Event{
		HookEventName: "PostToolUse",
		Timestamp:     now,
	}

	matches := engine.Evaluate(activities, currentEvent)
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
		name       string
		toolRegex  string
		toolName   string
		wantMatch  bool
	}{
		{"exact match", "Edit", "Edit", true},
		{"regex or", "Edit|Write", "Write", true},
		{"regex or no match", "Edit|Write", "Read", false},
		{"partial no match", "Edit", "EditFoo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.Rule{
				Name:    "tool-test",
				Enabled: true,
				Trigger: types.Trigger{
					Logic: "and",
					Conditions: []types.Condition{
						{Tool: tt.toolRegex},
					},
				},
				Action: types.Action{Type: types.ActionLog},
			}
			engine := NewEngine([]types.Rule{rule})
			activities := []types.Activity{
				makeActivity("PostToolUse", tt.toolName, nil, now.Add(-1*time.Minute), "s1"),
			}
			currentEvent := types.Event{Timestamp: now}
			matches := engine.Evaluate(activities, currentEvent)
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateFilePattern(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		pattern   string
		filePath  string
		wantMatch bool
	}{
		{"go test file", "*_test.go", "/foo/bar/service_test.go", true},
		{"ts test file", "*.test.ts", "/src/utils.test.ts", true},
		{"not test file", "*_test.go", "/foo/bar/service.go", false},
		{"pipe-separated patterns", "*_test.go|*.test.ts", "/src/thing.test.ts", true},
		{"no file_path in input", "*_test.go", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.Rule{
				Name:    "file-test",
				Enabled: true,
				Trigger: types.Trigger{
					Logic: "and",
					Conditions: []types.Condition{
						{FilePattern: tt.pattern},
					},
				},
				Action: types.Action{Type: types.ActionLog},
			}
			engine := NewEngine([]types.Rule{rule})

			var input map[string]any
			if tt.filePath != "" {
				input = fileInput(tt.filePath)
			}
			activities := []types.Activity{
				makeActivity("PostToolUse", "Edit", input, now.Add(-1*time.Minute), "s1"),
			}
			currentEvent := types.Event{Timestamp: now}
			matches := engine.Evaluate(activities, currentEvent)
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateCountThreshold(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		count      int
		numEvents  int
		wantMatch  bool
	}{
		{"exactly at threshold", 3, 3, true},
		{"above threshold", 3, 5, true},
		{"below threshold", 3, 2, false},
		{"zero count defaults to 1", 0, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.Rule{
				Name:    "count-test",
				Enabled: true,
				Trigger: types.Trigger{
					Logic: "and",
					Conditions: []types.Condition{
						{Event: "PostToolUse", Count: tt.count},
					},
				},
				Action: types.Action{Type: types.ActionLog},
			}
			engine := NewEngine([]types.Rule{rule})

			var activities []types.Activity
			for i := 0; i < tt.numEvents; i++ {
				activities = append(activities, makeActivity("PostToolUse", "Edit", nil, now.Add(-time.Duration(i)*time.Second), "s1"))
			}
			currentEvent := types.Event{Timestamp: now}
			matches := engine.Evaluate(activities, currentEvent)
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestEvaluateTimeWindow(t *testing.T) {
	now := time.Now()

	rule := types.Rule{
		Name:    "window-test",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse", Count: 2, Within: "5m"},
			},
		},
		Action: types.Action{Type: types.ActionLog},
	}

	t.Run("within window", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("outside window", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-10*time.Minute), "s1"), // outside 5m window
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestEvaluateNegate(t *testing.T) {
	now := time.Now()

	t.Run("negated condition passes when no matching activities", func(t *testing.T) {
		rule := types.Rule{
			Name:    "negate-pass",
			Enabled: true,
			Trigger: types.Trigger{
				Logic: "and",
				Conditions: []types.Condition{
					{Event: "PostToolUse", Tool: "Read", Negate: true, Count: 1, Within: "5m"},
				},
			},
			Action: types.Action{Type: types.ActionLog},
		}
		engine := NewEngine([]types.Rule{rule})
		// Activities have Edit but no Read.
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match (negated pass), got %d", len(matches))
		}
	})

	t.Run("negated condition fails when matching activities exist", func(t *testing.T) {
		rule := types.Rule{
			Name:    "negate-fail",
			Enabled: true,
			Trigger: types.Trigger{
				Logic: "and",
				Conditions: []types.Condition{
					{Event: "PostToolUse", Tool: "Read", Negate: true, Count: 1, Within: "5m"},
				},
			},
			Action: types.Action{Type: types.ActionLog},
		}
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Read", nil, now.Add(-1*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches (negated fail), got %d", len(matches))
		}
	})
}

func TestEvaluateAndLogic(t *testing.T) {
	now := time.Now()

	rule := types.Rule{
		Name:    "and-logic",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse", Tool: "Edit", Count: 2},
				{Event: "PostToolUse", Tool: "Bash", Count: 1},
			},
		},
		Action: types.Action{Type: types.ActionLog},
	}

	t.Run("both conditions met", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
			makeActivity("PostToolUse", "Bash", nil, now.Add(-3*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("only first condition met", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestEvaluateOrLogic(t *testing.T) {
	now := time.Now()

	rule := types.Rule{
		Name:    "or-logic",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "or",
			Conditions: []types.Condition{
				{Event: "PostToolUse", Tool: "Edit", Count: 3},
				{Event: "PostToolUse", Tool: "Write", Count: 1},
			},
		},
		Action: types.Action{Type: types.ActionLog},
	}

	t.Run("first condition met only", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-2*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", nil, now.Add(-3*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("second condition met only", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Write", nil, now.Add(-1*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 1 {
			t.Fatalf("expected 1 match, got %d", len(matches))
		}
	})

	t.Run("neither condition met", func(t *testing.T) {
		engine := NewEngine([]types.Rule{rule})
		activities := []types.Activity{
			makeActivity("PostToolUse", "Read", nil, now.Add(-1*time.Minute), "s1"),
		}
		currentEvent := types.Event{Timestamp: now}
		matches := engine.Evaluate(activities, currentEvent)
		if len(matches) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(matches))
		}
	})
}

func TestEvaluateDisabledRule(t *testing.T) {
	now := time.Now()

	rule := types.Rule{
		Name:    "disabled-rule",
		Enabled: false,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse"},
			},
		},
		Action: types.Action{Type: types.ActionLog},
	}
	engine := NewEngine([]types.Rule{rule})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	currentEvent := types.Event{Timestamp: now}
	matches := engine.Evaluate(activities, currentEvent)
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for disabled rule, got %d", len(matches))
	}
}

func TestEvaluatePrioritySort(t *testing.T) {
	now := time.Now()

	rules := []types.Rule{
		{
			Name:     "low-priority",
			Enabled:  true,
			Priority: 1,
			Trigger:  types.Trigger{Logic: "and", Conditions: []types.Condition{{Event: "PostToolUse"}}},
			Action:   types.Action{Type: types.ActionLog},
		},
		{
			Name:     "high-priority",
			Enabled:  true,
			Priority: 10,
			Trigger:  types.Trigger{Logic: "and", Conditions: []types.Condition{{Event: "PostToolUse"}}},
			Action:   types.Action{Type: types.ActionLog},
		},
		{
			Name:     "mid-priority",
			Enabled:  true,
			Priority: 5,
			Trigger:  types.Trigger{Logic: "and", Conditions: []types.Condition{{Event: "PostToolUse"}}},
			Action:   types.Action{Type: types.ActionLog},
		},
	}
	engine := NewEngine(rules)
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}
	currentEvent := types.Event{Timestamp: now}
	matches := engine.Evaluate(activities, currentEvent)
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

// --- FilePatternExclude unit tests ---

func TestEvaluateFilePatternExclude(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		exclude   string
		filePath  string
		wantMatch bool
	}{
		{"source file not excluded", "*_test.go", "/foo/handler.go", true},
		{"test file excluded", "*_test.go", "/foo/handler_test.go", false},
		{"no file_path treated as not excluded", "*_test.go", "", true},
		{"pipe-separated exclude", "*_test.go|*.test.ts", "/foo/handler.go", true},
		{"pipe-separated exclude matches", "*_test.go|*.test.ts", "/foo/app.test.ts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := types.Rule{
				Name:    "exclude-test",
				Enabled: true,
				Trigger: types.Trigger{
					Logic: "and",
					Conditions: []types.Condition{
						{Event: "PostToolUse", FilePatternExclude: tt.exclude},
					},
				},
				Action: types.Action{Type: types.ActionLog},
			}
			engine := NewEngine([]types.Rule{rule})
			var input map[string]any
			if tt.filePath != "" {
				input = fileInput(tt.filePath)
			}
			activities := []types.Activity{
				makeActivity("PostToolUse", "Edit", input, now.Add(-1*time.Minute), "s1"),
			}
			currentEvent := types.Event{Timestamp: now}
			matches := engine.Evaluate(activities, currentEvent)
			if got := len(matches) > 0; got != tt.wantMatch {
				t.Errorf("match = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

// --- Comprehensive Scenario Tests ---
// Tests all realistic Claude Code behavior paths against the test-only-modification rule.

func newTestOnlyModificationRule() types.Rule {
	return types.Rule{
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
					Event:  "PostToolUse",
					Tool:   "Read|Glob|Grep",
					Negate: true,
					Count:  1,
					Within: "5m",
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
}

func TestScenario_PureTestEdits_Blocked(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected BLOCKED (pure test edits), got %d matches", len(matches))
	}
}

func TestScenario_TestEdits_ThenReadSource_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Read", fileInput("/p/handler.go"), now.Add(-2*time.Minute), "s1"), // Read source
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (read source), got %d matches", len(matches))
	}
}

func TestScenario_TestEdits_ThenEditSource_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-2*time.Minute), "s1"), // Edit source
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (edited source), got %d matches", len(matches))
	}
}

func TestScenario_TestEdits_ThenGlobGrep_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})

	t.Run("Glob resets", func(t *testing.T) {
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
			makeActivity("PostToolUse", "Glob", nil, now.Add(-2*time.Minute), "s1"), // Glob search
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
		}
		matches := engine.Evaluate(activities, types.Event{Timestamp: now})
		if len(matches) != 0 {
			t.Fatalf("expected ALLOWED (Glob resets), got %d matches", len(matches))
		}
	})

	t.Run("Grep resets", func(t *testing.T) {
		activities := []types.Activity{
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
			makeActivity("PostToolUse", "Grep", nil, now.Add(-2*time.Minute), "s1"), // Grep search
			makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
		}
		matches := engine.Evaluate(activities, types.Event{Timestamp: now})
		if len(matches) != 0 {
			t.Fatalf("expected ALLOWED (Grep resets), got %d matches", len(matches))
		}
	})
}

func TestScenario_TestEdits_ThenBash_StillBlocked(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	// Bash alone (e.g. running tests) should NOT reset the rule
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Bash", nil, now.Add(-2*time.Minute), "s1"), // just running test command
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected BLOCKED (Bash doesn't reset), got %d matches", len(matches))
	}
}

func TestScenario_BelowThreshold_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 2 edits), got %d matches", len(matches))
	}
}

func TestScenario_OutsideTimeWindow_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-10*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-8*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-7*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (outside window), got %d matches", len(matches))
	}
}

func TestScenario_NonTestFileEdits_Allowed(t *testing.T) {
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/main.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (not test files), got %d matches", len(matches))
	}
}

func TestScenario_ReadTestFile_Allowed(t *testing.T) {
	// Reading the test file itself counts as "exploring code" — allowed
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Read", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"), // Read test file
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (read test file = exploring), got %d matches", len(matches))
	}
}

func TestScenario_MixedWorkflow_SourceEditResetsCounter(t *testing.T) {
	// Realistic: edit test, edit test, edit source (fix impl), edit test, edit test, edit test
	// The source edit should prevent triggering even though there are 5 test edits total
	now := time.Now()
	engine := NewEngine([]types.Rule{newTestOnlyModificationRule()})
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-5*time.Minute+10*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"), // Fix source
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-1*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler_test.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (source edit within window), got %d matches", len(matches))
	}
}

// --- Cooldown Tests ---

func TestCooldown_MatchThenSuppressThenMatchAgain(t *testing.T) {
	now := time.Now()
	rule := types.Rule{
		Name:    "cooldown-rule",
		Enabled: true,
		Trigger: types.Trigger{
			Logic: "and",
			Conditions: []types.Condition{
				{Event: "PostToolUse", Tool: "Bash", Count: 2, Within: "5m"},
			},
		},
		Action: types.Action{
			Type:     types.ActionBlock,
			Message:  "blocked",
			Cooldown: "30s",
		},
	}
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation: should match.
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("first eval: expected 1 match, got %d", len(matches))
	}

	// Second evaluation 10s later: within cooldown, should NOT match.
	activities = append(activities, makeActivity("PostToolUse", "Bash", nil, now.Add(5*time.Second), "s1"))
	matches = engine.Evaluate(activities, types.Event{Timestamp: now.Add(10 * time.Second)})
	if len(matches) != 0 {
		t.Fatalf("during cooldown: expected 0 matches, got %d", len(matches))
	}

	// Third evaluation 31s later: cooldown expired, should match again.
	activities = append(activities, makeActivity("PostToolUse", "Bash", nil, now.Add(30*time.Second), "s1"))
	matches = engine.Evaluate(activities, types.Event{Timestamp: now.Add(31 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("after cooldown: expected 1 match, got %d", len(matches))
	}
}

func TestCooldown_DoesNotAffectOtherRules(t *testing.T) {
	now := time.Now()
	ruleA := types.Rule{
		Name:    "rule-a",
		Enabled: true,
		Trigger: types.Trigger{
			Logic:      "and",
			Conditions: []types.Condition{{Event: "PostToolUse", Tool: "Bash"}},
		},
		Action: types.Action{
			Type:     types.ActionBlock,
			Message:  "blocked-a",
			Cooldown: "1m",
		},
	}
	ruleB := types.Rule{
		Name:    "rule-b",
		Enabled: true,
		Trigger: types.Trigger{
			Logic:      "and",
			Conditions: []types.Condition{{Event: "PostToolUse", Tool: "Bash"}},
		},
		Action: types.Action{
			Type:    types.ActionLog,
			Message: "logged-b",
			// No cooldown.
		},
	}
	engine := NewEngine([]types.Rule{ruleA, ruleB})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation: both should match.
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 2 {
		t.Fatalf("first eval: expected 2 matches, got %d", len(matches))
	}

	// Second evaluation 10s later: rule-a in cooldown, rule-b should still match.
	matches = engine.Evaluate(activities, types.Event{Timestamp: now.Add(10 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("during cooldown: expected 1 match, got %d", len(matches))
	}
	if matches[0].Rule.Name != "rule-b" {
		t.Errorf("expected rule-b to match, got %q", matches[0].Rule.Name)
	}
}

func TestCooldown_NoCooldown_AlwaysMatches(t *testing.T) {
	now := time.Now()
	rule := types.Rule{
		Name:    "no-cooldown",
		Enabled: true,
		Trigger: types.Trigger{
			Logic:      "and",
			Conditions: []types.Condition{{Event: "PostToolUse", Tool: "Edit"}},
		},
		Action: types.Action{
			Type:    types.ActionLog,
			Message: "logged",
			// No cooldown field — should always match.
		},
	}
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", nil, now.Add(-1*time.Minute), "s1"),
	}

	// Evaluate multiple times in quick succession: all should match.
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
	rule := types.Rule{
		Name:    "exact-expiry",
		Enabled: true,
		Trigger: types.Trigger{
			Logic:      "and",
			Conditions: []types.Condition{{Event: "PostToolUse", Tool: "Bash"}},
		},
		Action: types.Action{
			Type:     types.ActionBlock,
			Message:  "blocked",
			Cooldown: "30s",
		},
	}
	engine := NewEngine([]types.Rule{rule})

	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}

	// First evaluation: should match, sets cooldown expiry to now+30s.
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("first eval: expected 1 match, got %d", len(matches))
	}

	// Evaluate at exactly the cooldown expiry: now+30s is NOT before expiry, so should match.
	matches = engine.Evaluate(activities, types.Event{Timestamp: now.Add(30 * time.Second)})
	if len(matches) != 1 {
		t.Fatalf("at exact expiry: expected 1 match, got %d", len(matches))
	}
}

// --- matchFilePattern unit tests ---

func TestMatchFilePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
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

// --- Engine with no rules ---

func TestEvaluateNoRules(t *testing.T) {
	engine := NewEngine(nil)
	matches := engine.Evaluate(nil, types.Event{Timestamp: time.Now()})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches with no rules, got %d", len(matches))
	}
}

// --- Engine with empty activities ---

func TestEvaluateNoActivities(t *testing.T) {
	rule := types.Rule{
		Name:    "needs-activities",
		Enabled: true,
		Trigger: types.Trigger{
			Logic:      "and",
			Conditions: []types.Condition{{Event: "PostToolUse", Count: 1}},
		},
		Action: types.Action{Type: types.ActionLog},
	}
	engine := NewEngine([]types.Rule{rule})
	matches := engine.Evaluate(nil, types.Event{Timestamp: time.Now()})
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches with no activities, got %d", len(matches))
	}
}

// --- Helper to load actual default rules ---

func loadTestRules(t *testing.T) []types.Rule {
	t.Helper()
	projectRoot := filepath.Join("..", "..", "rules")
	rules, err := LoadRules(projectRoot)
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

// =============================================================================
// Scenario Tests: excessive-retry-same-command (priority 8)
// Triggers: 3+ PostToolUseFailure Bash events within 3 minutes
// =============================================================================

func TestScenario_ExcessiveRetry_ThreeFailures_Blocked(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-1*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected BLOCKED (3 Bash failures), got %d matches", len(matches))
	}
	if matches[0].Rule.Action.Type != types.ActionBlock {
		t.Errorf("expected block action, got %q", matches[0].Rule.Action.Type)
	}
}

func TestScenario_ExcessiveRetry_TwoFailures_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 2 failures), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveRetry_OutsideWindow_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 failures but spread beyond the 3-minute window
	activities := []types.Activity{
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-5*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-3*time.Minute-1*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (failures outside 3m window), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveRetry_NonBashFailures_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 failures but with Edit, not Bash
	activities := []types.Activity{
		makeActivity("PostToolUseFailure", "Edit", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Edit", nil, now.Add(-1*time.Minute), "s1"),
		makeActivity("PostToolUseFailure", "Edit", nil, now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Edit failures, not Bash), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveRetry_PostToolUseSuccess_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 Bash events but PostToolUse (success), not PostToolUseFailure
	activities := []types.Activity{
		makeActivity("PostToolUse", "Bash", nil, now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Bash", nil, now.Add(-1*time.Minute), "s1"),
		makeActivity("PostToolUse", "Bash", nil, now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (success events, not failures), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveRetry_FourFailures_Blocked(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "excessive-retry-same-command")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-150*time.Second), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-100*time.Second), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-50*time.Second), "s1"),
		makeActivity("PostToolUseFailure", "Bash", nil, now.Add(-10*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected BLOCKED (4 failures exceeds threshold), got %d matches", len(matches))
	}
}

// =============================================================================
// Scenario Tests: blind-file-creation (priority 5)
// Triggers: 3+ Write events + zero Read/Glob/Grep events within 5 minutes
// =============================================================================

func TestScenario_BlindFileCreation_ThreeWrites_NoReads_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (3 writes, no reads), got %d matches", len(matches))
	}
	if matches[0].Rule.Action.Type != types.ActionInject {
		t.Errorf("expected inject action, got %q", matches[0].Rule.Action.Type)
	}
}

func TestScenario_BlindFileCreation_WritesAfterRead_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Read", fileInput("/p/existing.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Read present), got %d matches", len(matches))
	}
}

func TestScenario_BlindFileCreation_WritesAfterGlob_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Glob", nil, now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Glob present), got %d matches", len(matches))
	}
}

func TestScenario_BlindFileCreation_WritesAfterGrep_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Grep", nil, now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Grep present), got %d matches", len(matches))
	}
}

func TestScenario_BlindFileCreation_TwoWrites_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 2 writes), got %d matches", len(matches))
	}
}

func TestScenario_BlindFileCreation_OutsideWindow_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 writes but all outside the 5-minute window
	activities := []types.Activity{
		makeActivity("PostToolUse", "Write", fileInput("/p/new1.go"), now.Add(-8*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new2.go"), now.Add(-7*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new3.go"), now.Add(-6*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (writes outside 5m window), got %d matches", len(matches))
	}
}

func TestScenario_BlindFileCreation_EditsDontCount_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "blind-file-creation")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 Edits (not Writes) — blind-file-creation only tracks Write
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/file1.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/file2.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/file3.go"), now.Add(-1*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Edit, not Write), got %d matches", len(matches))
	}
}

// =============================================================================
// Scenario Tests: same-file-excessive-edits (priority 6)
// Triggers: 8+ Edit/Write events within 5 minutes
// =============================================================================

func TestScenario_ExcessiveEdits_EightEdits_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	var activities []types.Activity
	for i := 0; i < 8; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(8-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (8 edits), got %d matches", len(matches))
	}
	if matches[0].Rule.Action.Type != types.ActionInject {
		t.Errorf("expected inject action, got %q", matches[0].Rule.Action.Type)
	}
}

func TestScenario_ExcessiveEdits_SevenEdits_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	var activities []types.Activity
	for i := 0; i < 7; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(7-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 7 edits), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveEdits_OutsideWindow_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 8 edits but all outside the 5-minute window
	var activities []types.Activity
	for i := 0; i < 8; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-6*time.Minute-time.Duration(i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (edits outside 5m window), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveEdits_MixedEditWrite_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// Mix of Edit and Write totaling 8
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-210*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-3*time.Minute), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/other.go"), now.Add(-150*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-2*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/config.go"), now.Add(-1*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (8 mixed Edit/Write), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveEdits_TenEdits_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	var activities []types.Activity
	for i := 0; i < 10; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(10-i)*20*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (10 edits exceeds threshold), got %d matches", len(matches))
	}
}

func TestScenario_ExcessiveEdits_ReadsDontCount_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "same-file-excessive-edits")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 7 edits + 3 reads = only 7 Edit/Write events
	var activities []types.Activity
	for i := 0; i < 7; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(10-i)*20*time.Second), "s1",
		))
	}
	for i := 0; i < 3; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Read", fileInput("/p/handler.go"),
			now.Add(-time.Duration(3-i)*15*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 7 Edit/Write, Reads dont count), got %d matches", len(matches))
	}
}

// =============================================================================
// Scenario Tests: write-before-read (priority 4)
// Triggers: 3+ Edit/Write events + zero Read/Glob/Grep events within 2 minutes
// =============================================================================

func TestScenario_WriteBeforeRead_ThreeEdits_NoReads_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (3 edits, no reads), got %d matches", len(matches))
	}
	if matches[0].Rule.Action.Type != types.ActionInject {
		t.Errorf("expected inject action, got %q", matches[0].Rule.Action.Type)
	}
}

func TestScenario_WriteBeforeRead_WritesAfterGrep_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Grep", nil, now.Add(-100*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Grep present), got %d matches", len(matches))
	}
}

func TestScenario_WriteBeforeRead_WritesAfterRead_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Read", fileInput("/p/existing.go"), now.Add(-100*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Read present), got %d matches", len(matches))
	}
}

func TestScenario_WriteBeforeRead_OutsideWindow_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 3 edits but outside the 2-minute window
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-5*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-4*time.Minute), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-3*time.Minute), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (edits outside 2m window), got %d matches", len(matches))
	}
}

func TestScenario_WriteBeforeRead_TwoEdits_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 2 edits), got %d matches", len(matches))
	}
}

func TestScenario_WriteBeforeRead_MixedEditWrite_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// Mix of Edit and Write, no reads
	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-90*time.Second), "s1"),
		makeActivity("PostToolUse", "Write", fileInput("/p/new.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (3 mixed Edit/Write, no reads), got %d matches", len(matches))
	}
}

func TestScenario_WriteBeforeRead_GlobResets_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "write-before-read")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	activities := []types.Activity{
		makeActivity("PostToolUse", "Edit", fileInput("/p/handler.go"), now.Add(-100*time.Second), "s1"),
		makeActivity("PostToolUse", "Glob", nil, now.Add(-80*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/service.go"), now.Add(-60*time.Second), "s1"),
		makeActivity("PostToolUse", "Edit", fileInput("/p/model.go"), now.Add(-30*time.Second), "s1"),
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (Glob present), got %d matches", len(matches))
	}
}

// =============================================================================
// Scenario Tests: session-context-warning (priority 2)
// Triggers: 50+ PostToolUse events within 30 minutes
// =============================================================================

func TestScenario_SessionContext_FiftyEvents_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	var activities []types.Activity
	for i := 0; i < 50; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(50-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (50 events), got %d matches", len(matches))
	}
	if matches[0].Rule.Action.Type != types.ActionInject {
		t.Errorf("expected inject action, got %q", matches[0].Rule.Action.Type)
	}
}

func TestScenario_SessionContext_FortyNineEvents_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	var activities []types.Activity
	for i := 0; i < 49; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(49-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 49 events), got %d matches", len(matches))
	}
}

func TestScenario_SessionContext_OutsideWindow_Allowed(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 50 events but all outside the 30-minute window
	var activities []types.Activity
	for i := 0; i < 50; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-31*time.Minute-time.Duration(i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (events outside 30m window), got %d matches", len(matches))
	}
}

func TestScenario_SessionContext_MixedTools_Triggered(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// session-context-warning has no tool filter — any PostToolUse counts
	tools := []string{"Edit", "Write", "Read", "Bash", "Grep", "Glob"}
	var activities []types.Activity
	for i := 0; i < 50; i++ {
		tool := tools[i%len(tools)]
		activities = append(activities, makeActivity(
			"PostToolUse", tool, nil,
			now.Add(-time.Duration(50-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 1 {
		t.Fatalf("expected TRIGGERED (50 mixed tool events), got %d matches", len(matches))
	}
}

func TestScenario_SessionContext_SplitAcrossWindow_PartiallyCounted(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 30 events inside window + 25 events outside window = only 30 count
	var activities []types.Activity
	for i := 0; i < 25; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-35*time.Minute-time.Duration(i)*time.Minute), "s1",
		))
	}
	for i := 0; i < 30; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", fileInput("/p/handler.go"),
			now.Add(-time.Duration(30-i)*time.Minute+30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 30 events in window), got %d matches", len(matches))
	}
}

func TestScenario_SessionContext_PreToolUse_NotCounted(t *testing.T) {
	rules := loadTestRules(t)
	rule := findRule(t, rules, "session-context-warning")
	engine := NewEngine([]types.Rule{rule})
	now := time.Now()

	// 30 PostToolUse + 25 PreToolUse — only PostToolUse events match the condition
	var activities []types.Activity
	for i := 0; i < 30; i++ {
		activities = append(activities, makeActivity(
			"PostToolUse", "Edit", nil,
			now.Add(-time.Duration(50-i)*30*time.Second), "s1",
		))
	}
	for i := 0; i < 25; i++ {
		activities = append(activities, makeActivity(
			"PreToolUse", "Edit", nil,
			now.Add(-time.Duration(25-i)*30*time.Second), "s1",
		))
	}
	matches := engine.Evaluate(activities, types.Event{Timestamp: now})
	if len(matches) != 0 {
		t.Fatalf("expected ALLOWED (only 30 PostToolUse events), got %d matches", len(matches))
	}
}
