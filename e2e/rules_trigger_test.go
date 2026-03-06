// Package e2e_test provides end-to-end tests that verify each rule in
// rules/default.yaml triggers correctly through the full HTTP stack:
// HTTP request → Server → Tracker → Engine → Executor → HTTP response.
package e2e_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/action"
	"github.com/Jack-Lin-DS-AI/squawk/internal/monitor"
	"github.com/Jack-Lin-DS-AI/squawk/internal/rules"
	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// --- Helpers ---

// rulesDir returns the absolute path to the project's rules directory.
func rulesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "rules"))
	if err != nil {
		t.Fatalf("failed to resolve rules dir: %v", err)
	}
	return dir
}

// loadAllRules loads all rules from the project's rules/default.yaml.
func loadAllRules(t *testing.T) []types.Rule {
	t.Helper()
	loaded, err := rules.LoadRules(rulesDir(t))
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no rules loaded")
	}
	return loaded
}

// singleRuleEngine creates an Engine with only the named rule enabled.
func singleRuleEngine(t *testing.T, allRules []types.Rule, name string) *rules.Engine {
	t.Helper()
	for _, r := range allRules {
		if r.Name == name {
			r.Enabled = true
			return rules.NewEngine([]types.Rule{r})
		}
	}
	t.Fatalf("rule %q not found in loaded rules", name)
	return nil
}

// testServer creates a full-stack test server with a single rule enabled,
// a real Tracker, and a real LoggingExecutor (writing to a temp log file).
func testServer(t *testing.T, allRules []types.Rule, ruleName string) *httptest.Server {
	t.Helper()
	engine := singleRuleEngine(t, allRules, ruleName)
	tracker := monitor.NewTracker(60 * time.Minute)

	logFile := filepath.Join(t.TempDir(), "squawk.log")
	logger, err := action.NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	t.Cleanup(func() { logger.Close() })

	executor := action.NewLoggingExecutor(
		action.NewExecutor(log.New(os.Stderr, "squawk-test: ", log.LstdFlags)),
		logger,
	)

	srv := monitor.NewServer(":0", "", tracker, engine, executor, "")
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

// postEvent sends an event to the given path and returns the decoded HookResponse.
func postEvent(t *testing.T, ts *httptest.Server, path string, event types.Event) types.HookResponse {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s returned status %d", path, resp.StatusCode)
	}
	var hr types.HookResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return hr
}

// postToolUse sends a PostToolUse event and returns the response.
func postToolUse(t *testing.T, ts *httptest.Server, event types.Event) types.HookResponse {
	t.Helper()
	return postEvent(t, ts, "/hooks/post-tool-use", event)
}

// preToolUse sends a PreToolUse event and returns the response.
func preToolUse(t *testing.T, ts *httptest.Server, event types.Event) types.HookResponse {
	t.Helper()
	return postEvent(t, ts, "/hooks/pre-tool-use", event)
}

// makeEvent creates an Event with the given parameters.
func makeEvent(session, hookName, tool string, ts time.Time, input map[string]any) types.Event {
	return types.Event{
		SessionID:     session,
		HookEventName: hookName,
		ToolName:      tool,
		ToolInput:     input,
		Timestamp:     ts,
	}
}

// --- E2E Rule Trigger Tests ---

func TestRuleTriggers_E2E(t *testing.T) {
	allRules := loadAllRules(t)

	t.Run("test-only-modification", func(t *testing.T) {
		ts := testServer(t, allRules, "test-only-modification")
		now := time.Now()
		sid := "sess-test-only-mod"

		// Send 3 PostToolUse Edit events on test files (no Read/Glob/Grep).
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/foo_test.go", "old_string": "old", "new_string": "new"}))
		}

		// PreToolUse Edit on a test file — should be blocked.
		// Timestamp after cooldown (30s) expires.
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/bar_test.go"}))

		if hr.Decision != "block" {
			t.Errorf("expected block, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("test-only-modification_allows_when_reads_exist", func(t *testing.T) {
		ts := testServer(t, allRules, "test-only-modification")
		now := time.Now()
		sid := "sess-test-only-mod-neg"

		// Send 3 Edit events on test files.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/foo_test.go", "old_string": "old", "new_string": "new"}))
		}

		// Also send a Read event (breaks negated condition).
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
			now.Add(3*time.Second),
			map[string]any{"file_path": "/project/foo.go"}))

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/bar_test.go"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (Read exists), got block: %q", hr.Reason)
		}
	})

	t.Run("blind-file-creation", func(t *testing.T) {
		ts := testServer(t, allRules, "blind-file-creation")
		now := time.Now()
		sid := "sess-blind-create"

		// Send 2 Write events (no reads).
		for i := range 2 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/new_file_" + string(rune('a'+i)) + ".go", "content": "package main"}))
		}

		// 3rd Write — should trigger inject.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
			now.Add(3*time.Second),
			map[string]any{"file_path": "/project/new_file_c.go", "content": "package main"}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject (additionalContext), got empty response")
		}
	})

	t.Run("blind-file-creation_allows_when_reads_exist", func(t *testing.T) {
		ts := testServer(t, allRules, "blind-file-creation")
		now := time.Now()
		sid := "sess-blind-create-neg"

		// Send a Read event first.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
			now,
			map[string]any{"file_path": "/project/existing.go"}))

		// Send 3 Write events.
		for i := range 3 {
			hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
				now.Add(time.Duration(i+1)*time.Second),
				map[string]any{"file_path": "/project/new_" + string(rune('a'+i)) + ".go", "content": "package main"}))

			if hr.AdditionalContext != "" {
				t.Errorf("expected allow (Read exists), got inject on Write #%d: %q", i+1, hr.AdditionalContext)
			}
		}
	})

	t.Run("same-file-excessive-edits", func(t *testing.T) {
		ts := testServer(t, allRules, "same-file-excessive-edits")
		now := time.Now()
		sid := "sess-excessive-edits"

		// Send 7 Edit events.
		for i := range 7 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/main.go", "old_string": "v" + string(rune('0'+i)), "new_string": "v" + string(rune('1'+i))}))
		}

		// 8th Edit — should trigger inject.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(8*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "v7", "new_string": "v8"}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject after 8 edits, got empty response")
		}
	})

	t.Run("same-file-excessive-edits_allows_below_threshold", func(t *testing.T) {
		ts := testServer(t, allRules, "same-file-excessive-edits")
		now := time.Now()
		sid := "sess-excessive-edits-neg"

		// Only 7 edits — below threshold of 8.
		for i := range 7 {
			hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/main.go", "old_string": "old", "new_string": "new" + string(rune('0'+i))}))

			if hr.AdditionalContext != "" {
				t.Errorf("expected allow at edit #%d, got inject: %q", i+1, hr.AdditionalContext)
			}
		}
	})

	t.Run("write-before-read", func(t *testing.T) {
		ts := testServer(t, allRules, "write-before-read")
		now := time.Now()
		sid := "sess-write-before-read"

		// Send 2 Edit events (no reads, within 2m).
		for i := range 2 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/main.go", "old_string": "a", "new_string": "b"}))
		}

		// 3rd Edit — should trigger inject.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(3*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "b", "new_string": "c"}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject after 3 edits without reading, got empty response")
		}
	})

	t.Run("write-before-read_allows_when_reads_exist", func(t *testing.T) {
		ts := testServer(t, allRules, "write-before-read")
		now := time.Now()
		sid := "sess-write-before-read-neg"

		// Read first.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Grep",
			now,
			map[string]any{"pattern": "TODO"}))

		// 3 Edits within 2m.
		for i := range 3 {
			hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i+1)*time.Second),
				map[string]any{"file_path": "/project/main.go", "old_string": "x", "new_string": "y"}))

			if hr.AdditionalContext != "" {
				t.Errorf("expected allow (Grep exists), got inject on edit #%d: %q", i+1, hr.AdditionalContext)
			}
		}
	})

	t.Run("session-context-warning", func(t *testing.T) {
		ts := testServer(t, allRules, "session-context-warning")
		now := time.Now()
		sid := "sess-context-warning"

		// Send 49 PostToolUse events.
		for i := range 49 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/file.go"}))
		}

		// 50th event — should trigger inject.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
			now.Add(50*time.Second),
			map[string]any{"file_path": "/project/file.go"}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject after 50 tool interactions, got empty response")
		}
	})

	t.Run("session-context-warning_allows_below_threshold", func(t *testing.T) {
		ts := testServer(t, allRules, "session-context-warning")
		now := time.Now()
		sid := "sess-context-warning-neg"

		// 49 events — below threshold of 50.
		var lastHR types.HookResponse
		for i := range 49 {
			lastHR = postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/file.go"}))
		}

		if lastHR.AdditionalContext != "" {
			t.Errorf("expected allow at 49 events, got inject: %q", lastHR.AdditionalContext)
		}
	})

	t.Run("edit-oscillation", func(t *testing.T) {
		ts := testServer(t, allRules, "edit-oscillation")
		now := time.Now()
		sid := "sess-edit-oscillation"

		// A→B→A pattern on the same file.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{"file_path": "/project/main.go", "old_string": "original", "new_string": "version_A"}))

		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "version_A", "new_string": "version_B"}))

		// Revert to version_A — oscillation detected.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(2*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "version_B", "new_string": "version_A"}))

		// PreToolUse Edit — should be blocked (after 30s cooldown).
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision != "block" {
			t.Errorf("expected block for oscillation, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("edit-oscillation_allows_without_reversion", func(t *testing.T) {
		ts := testServer(t, allRules, "edit-oscillation")
		now := time.Now()
		sid := "sess-edit-oscillation-neg"

		// A→B→C — no reversion.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{"file_path": "/project/main.go", "old_string": "original", "new_string": "version_A"}))

		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "version_A", "new_string": "version_B"}))

		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(2*time.Second),
			map[string]any{"file_path": "/project/main.go", "old_string": "version_B", "new_string": "version_C"}))

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(5*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (no reversion), got block: %q", hr.Reason)
		}
	})

	t.Run("repeated-identical-edit", func(t *testing.T) {
		ts := testServer(t, allRules, "repeated-identical-edit")
		now := time.Now()
		sid := "sess-repeated-edit"

		// Same (file, old_string, new_string) 3 times.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/main.go", "old_string": "foo", "new_string": "bar"}))
		}

		// PreToolUse Edit — should be blocked (after 30s cooldown).
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision != "block" {
			t.Errorf("expected block for repeated edit, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("repeated-identical-edit_allows_different_edits", func(t *testing.T) {
		ts := testServer(t, allRules, "repeated-identical-edit")
		now := time.Now()
		sid := "sess-repeated-edit-neg"

		// 3 different edits — not identical.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/main.go",
					"old_string": "old_" + string(rune('a'+i)),
					"new_string": "new_" + string(rune('a'+i))}))
		}

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(5*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (different edits), got block: %q", hr.Reason)
		}
	})

	t.Run("repeated-failing-command", func(t *testing.T) {
		ts := testServer(t, allRules, "repeated-failing-command")
		now := time.Now()
		sid := "sess-repeated-fail"

		// Same failing command 3 times.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUseFailure", "Bash",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"command": "npm test"}))
		}

		// PreToolUse Bash — should be blocked (after 60s cooldown).
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Bash",
			now.Add(63*time.Second),
			map[string]any{"command": "npm test"}))

		if hr.Decision != "block" {
			t.Errorf("expected block for repeated failing command, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("repeated-failing-command_allows_different_commands", func(t *testing.T) {
		ts := testServer(t, allRules, "repeated-failing-command")
		now := time.Now()
		sid := "sess-repeated-fail-neg"

		// 3 different failing commands.
		cmds := []string{"npm test", "go build ./...", "make lint"}
		for i, cmd := range cmds {
			postToolUse(t, ts, makeEvent(sid, "PostToolUseFailure", "Bash",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"command": cmd}))
		}

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Bash",
			now.Add(5*time.Second),
			map[string]any{"command": "npm test"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (different commands), got block: %q", hr.Reason)
		}
	})

	t.Run("whole-file-rewrite", func(t *testing.T) {
		ts := testServer(t, allRules, "whole-file-rewrite")
		now := time.Now()
		sid := "sess-whole-rewrite"

		// Read a file first (makes it "known").
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Read",
			now,
			map[string]any{"file_path": "/project/main.go"}))

		// Write to known file.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
			now.Add(1*time.Second),
			map[string]any{"file_path": "/project/main.go", "content": "rewrite 1"}))

		// 2nd Write to known file — should trigger inject.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
			now.Add(2*time.Second),
			map[string]any{"file_path": "/project/main.go", "content": "rewrite 2"}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject for whole-file rewrite, got empty response")
		}
	})

	t.Run("whole-file-rewrite_allows_new_files", func(t *testing.T) {
		ts := testServer(t, allRules, "whole-file-rewrite")
		now := time.Now()
		sid := "sess-whole-rewrite-neg"

		// Write to unknown files (never read).
		for i := range 3 {
			hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Write",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{"file_path": "/project/brand_new_" + string(rune('a'+i)) + ".go", "content": "new file"}))

			if hr.AdditionalContext != "" {
				t.Errorf("expected allow (unknown file), got inject on Write #%d: %q", i+1, hr.AdditionalContext)
			}
		}
	})

	t.Run("test-assertion-weakening", func(t *testing.T) {
		ts := testServer(t, allRules, "test-assertion-weakening")
		now := time.Now()
		sid := "sess-assert-weaken"

		// Edit test file: remove assertion (old has assert, new doesn't).
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{
				"file_path":  "/project/foo_test.go",
				"old_string": "assert.Equal(t, expected, got)",
				"new_string": "// removed",
			}))

		// 2nd removal of assertion.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{
				"file_path":  "/project/foo_test.go",
				"old_string": "require.NoError(t, err)",
				"new_string": "_ = err",
			}))

		// PreToolUse Edit on test file — should be blocked (after 30s cooldown).
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/foo_test.go"}))

		if hr.Decision != "block" {
			t.Errorf("expected block for assertion weakening, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("test-assertion-weakening_allows_when_assertions_kept", func(t *testing.T) {
		ts := testServer(t, allRules, "test-assertion-weakening")
		now := time.Now()
		sid := "sess-assert-weaken-neg"

		// Edit test file but keep assertions in both old and new.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{
				"file_path":  "/project/foo_test.go",
				"old_string": "assert.Equal(t, 1, got)",
				"new_string": "assert.Equal(t, 2, got)",
			}))

		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{
				"file_path":  "/project/foo_test.go",
				"old_string": "require.NoError(t, err)",
				"new_string": "require.Error(t, err)",
			}))

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(5*time.Second),
			map[string]any{"file_path": "/project/foo_test.go"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (assertions kept), got block: %q", hr.Reason)
		}
	})

	t.Run("error-handling-removal", func(t *testing.T) {
		ts := testServer(t, allRules, "error-handling-removal")
		now := time.Now()
		sid := "sess-err-removal"

		// Edit: remove error handling (old has "if err != nil", new doesn't).
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{
				"file_path":  "/project/main.go",
				"old_string": "if err != nil {\n    return err\n}",
				"new_string": "",
			}))

		// 2nd removal.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{
				"file_path":  "/project/handler.go",
				"old_string": "if err != nil {\n    log.Fatal(err)\n}",
				"new_string": "// ignore",
			}))

		// PreToolUse Edit — should be blocked (after 30s cooldown).
		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(33*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision != "block" {
			t.Errorf("expected block for error handling removal, got decision=%q reason=%q", hr.Decision, hr.Reason)
		}
	})

	t.Run("error-handling-removal_allows_when_errors_kept", func(t *testing.T) {
		ts := testServer(t, allRules, "error-handling-removal")
		now := time.Now()
		sid := "sess-err-removal-neg"

		// Edit with error handling in both old and new.
		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now,
			map[string]any{
				"file_path":  "/project/main.go",
				"old_string": "if err != nil {\n    return err\n}",
				"new_string": "if err != nil {\n    return fmt.Errorf(\"wrap: %w\", err)\n}",
			}))

		postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(1*time.Second),
			map[string]any{
				"file_path":  "/project/handler.go",
				"old_string": "if err != nil {\n    log.Fatal(err)\n}",
				"new_string": "if err != nil {\n    return err\n}",
			}))

		hr := preToolUse(t, ts, makeEvent(sid, "PreToolUse", "Edit",
			now.Add(5*time.Second),
			map[string]any{"file_path": "/project/main.go"}))

		if hr.Decision == "block" {
			t.Errorf("expected allow (error handling kept), got block: %q", hr.Reason)
		}
	})

	t.Run("large-code-deletion", func(t *testing.T) {
		ts := testServer(t, allRules, "large-code-deletion")
		now := time.Now()
		sid := "sess-large-deletion"

		// 3 edits where new_string is <50% of old_string length.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{
					"file_path":  "/project/main.go",
					"old_string": "this is a very long string that will be dramatically shortened in the edit operation number " + string(rune('0'+i)),
					"new_string": "short",
				}))
		}

		// The 3rd event should have triggered inject.
		// Send one more to verify the pattern persists.
		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(4*time.Second),
			map[string]any{
				"file_path":  "/project/main.go",
				"old_string": "another long string that is going to be replaced with something much smaller for testing",
				"new_string": "tiny",
			}))

		if hr.AdditionalContext == "" {
			t.Error("expected inject for large code deletion, got empty response")
		}
	})

	t.Run("large-code-deletion_allows_small_changes", func(t *testing.T) {
		ts := testServer(t, allRules, "large-code-deletion")
		now := time.Now()
		sid := "sess-large-deletion-neg"

		// 3 edits where new_string is similar length to old_string.
		for i := range 3 {
			postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
				now.Add(time.Duration(i)*time.Second),
				map[string]any{
					"file_path":  "/project/main.go",
					"old_string": "hello world " + string(rune('0'+i)),
					"new_string": "hello earth " + string(rune('0'+i)),
				}))
		}

		hr := postToolUse(t, ts, makeEvent(sid, "PostToolUse", "Edit",
			now.Add(4*time.Second),
			map[string]any{
				"file_path":  "/project/main.go",
				"old_string": "foo bar baz",
				"new_string": "foo qux baz",
			}))

		if hr.AdditionalContext != "" {
			t.Errorf("expected allow (similar-size edits), got inject: %q", hr.AdditionalContext)
		}
	})
}
