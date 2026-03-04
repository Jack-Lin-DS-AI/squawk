package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// --- Test doubles ---

type mockEvaluator struct {
	matches  []types.RuleMatch
	replaced []types.Rule
}

func (m *mockEvaluator) Evaluate(_ []types.Activity, _ types.Event) []types.RuleMatch {
	return m.matches
}

func (m *mockEvaluator) ReplaceRules(newRules []types.Rule) {
	m.replaced = newRules
}

type mockExecutor struct {
	calls []types.RuleMatch
	resp  *types.HookResponse
	err   error
}

func (m *mockExecutor) Execute(match types.RuleMatch) (*types.HookResponse, error) {
	m.calls = append(m.calls, match)
	return m.resp, m.err
}

type mockExecutorFunc struct {
	fn func(types.RuleMatch) (*types.HookResponse, error)
}

func (m *mockExecutorFunc) Execute(match types.RuleMatch) (*types.HookResponse, error) {
	return m.fn(match)
}

// --- Helpers ---

func startTestServer(t *testing.T, evaluator RuleEvaluator, executors ...ActionExecutor) (*httptest.Server, *Tracker) {
	t.Helper()
	tracker := NewTracker(10 * time.Minute)
	var exec ActionExecutor
	if len(executors) > 0 {
		exec = executors[0]
	}
	srv := NewServer(":0", "", tracker, evaluator, exec, "")
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, tracker
}

func makeMatch(name string, at types.ActionType, msg string) types.RuleMatch {
	return types.RuleMatch{
		Rule: types.Rule{
			Name:    name,
			Enabled: true,
			Action:  types.Action{Type: at, Message: msg},
		},
		MatchedAt: time.Now(),
	}
}

func makeEvent(sessionID, eventName, toolName string, input ...map[string]any) types.Event {
	e := types.Event{
		SessionID:     sessionID,
		HookEventName: eventName,
		ToolName:      toolName,
		Timestamp:     time.Now(),
	}
	if len(input) > 0 {
		e.ToolInput = input[0]
	}
	return e
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return v
}

// --- Server Tests ---

func TestServer(t *testing.T) {
	t.Run("health endpoint", func(t *testing.T) {
		ts, _ := startTestServer(t, &mockEvaluator{})
		resp, err := ts.Client().Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health failed: %v", err)
		}
		body := decodeJSON[map[string]string](t, resp)
		if body["status"] != "ok" {
			t.Errorf("status = %q, want %q", body["status"], "ok")
		}
	})

	t.Run("status endpoint with no sessions", func(t *testing.T) {
		ts, _ := startTestServer(t, &mockEvaluator{})
		resp, err := ts.Client().Get(ts.URL + "/status")
		if err != nil {
			t.Fatalf("GET /status failed: %v", err)
		}
		body := decodeJSON[statusResponse](t, resp)
		if len(body.Sessions) != 0 {
			t.Errorf("sessions = %v, want empty", body.Sessions)
		}
	})

	t.Run("post-tool-use records activity", func(t *testing.T) {
		ts, tracker := startTestServer(t, &mockEvaluator{})
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-1", "PostToolUse", "Edit"))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		resp.Body.Close()

		activities := tracker.GetActivities("sess-1")
		if len(activities) != 1 {
			t.Fatalf("activity count = %d, want 1", len(activities))
		}
		if activities[0].Event.ToolName != "Edit" {
			t.Errorf("tool_name = %q, want %q", activities[0].Event.ToolName, "Edit")
		}
	})

	t.Run("pre-tool-use allows when no rules match", func(t *testing.T) {
		ts, _ := startTestServer(t, &mockEvaluator{})
		resp := postJSON(t, ts, "/hooks/pre-tool-use", makeEvent("sess-2", "PreToolUse", "Read"))
		body := decodeJSON[types.HookResponse](t, resp)
		if body.Decision != "" {
			t.Errorf("decision = %q, want empty (allow)", body.Decision)
		}
	})

	t.Run("pre-tool-use blocks when block rule matches", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("block-dangerous-tool", types.ActionBlock, "tool usage blocked by policy")},
		}
		ts, _ := startTestServer(t, eval)
		resp := postJSON(t, ts, "/hooks/pre-tool-use",
			makeEvent("sess-3", "PreToolUse", "Bash", map[string]any{"command": "rm -rf /"}))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "block" {
			t.Errorf("decision = %q, want %q", body.Decision, "block")
		}
		if body.Reason != "tool usage blocked by policy" {
			t.Errorf("reason = %q, want %q", body.Reason, "tool usage blocked by policy")
		}
	})

	t.Run("generic event handler records activity", func(t *testing.T) {
		ts, tracker := startTestServer(t, &mockEvaluator{})
		resp := postJSON(t, ts, "/hooks/event", makeEvent("sess-4", "Notification", ""))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		resp.Body.Close()

		if len(tracker.GetActivities("sess-4")) != 1 {
			t.Fatal("expected 1 activity for sess-4")
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		ts, _ := startTestServer(t, &mockEvaluator{})
		resp, err := ts.Client().Post(
			ts.URL+"/hooks/pre-tool-use", "application/json",
			bytes.NewReader([]byte(`{invalid`)),
		)
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("status reflects recorded sessions", func(t *testing.T) {
		ts, _ := startTestServer(t, &mockEvaluator{})
		for _, sid := range []string{"sess-a", "sess-a", "sess-b"} {
			resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent(sid, "PostToolUse", "Read"))
			resp.Body.Close()
		}

		resp, err := ts.Client().Get(ts.URL + "/status")
		if err != nil {
			t.Fatalf("GET /status failed: %v", err)
		}
		body := decodeJSON[statusResponse](t, resp)
		if body.Sessions["sess-a"] != 2 {
			t.Errorf("sess-a count = %d, want 2", body.Sessions["sess-a"])
		}
		if body.Sessions["sess-b"] != 1 {
			t.Errorf("sess-b count = %d, want 1", body.Sessions["sess-b"])
		}
	})

	t.Run("post-tool-use with non-block rule does not block", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("notify-frequent-edits", types.ActionNotify, "lots of edits detected")},
		}
		ts, _ := startTestServer(t, eval)
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-5", "PostToolUse", "Edit"))
		body := decodeJSON[types.HookResponse](t, resp)
		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
	})

	t.Run("post-tool-use calls executor for each match", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{
				makeMatch("notify-edits", types.ActionNotify, "edit detected"),
				makeMatch("log-edits", types.ActionLog, "edit logged"),
			},
		}
		exec := &mockExecutor{}
		ts, _ := startTestServer(t, eval, exec)
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-exec-1", "PostToolUse", "Edit"))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
		if len(exec.calls) != 2 {
			t.Fatalf("executor call count = %d, want 2", len(exec.calls))
		}
		if exec.calls[0].Rule.Name != "notify-edits" {
			t.Errorf("first call rule = %q, want %q", exec.calls[0].Rule.Name, "notify-edits")
		}
		if exec.calls[1].Rule.Name != "log-edits" {
			t.Errorf("second call rule = %q, want %q", exec.calls[1].Rule.Name, "log-edits")
		}
	})

	t.Run("pre-tool-use block uses executor response", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("block-dangerous", types.ActionBlock, "original message")},
		}
		exec := &mockExecutor{
			resp: &types.HookResponse{Decision: "block", Reason: "expanded by executor"},
		}
		ts, _ := startTestServer(t, eval, exec)
		resp := postJSON(t, ts, "/hooks/pre-tool-use",
			makeEvent("sess-exec-2", "PreToolUse", "Bash", map[string]any{"command": "rm -rf /"}))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "block" {
			t.Errorf("decision = %q, want %q", body.Decision, "block")
		}
		if body.Reason != "expanded by executor" {
			t.Errorf("reason = %q, want %q", body.Reason, "expanded by executor")
		}
		if len(exec.calls) != 1 {
			t.Fatalf("executor call count = %d, want 1", len(exec.calls))
		}
	})

	t.Run("post-tool-use inject match returns additionalContext", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("inject-guidance", types.ActionInject, "you should read files first")},
		}
		exec := &mockExecutor{
			resp: &types.HookResponse{AdditionalContext: "you should read files first"},
		}
		ts, _ := startTestServer(t, eval, exec)
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-inject-1", "PostToolUse", "Edit"))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
		if body.AdditionalContext != "you should read files first" {
			t.Errorf("additionalContext = %q, want %q", body.AdditionalContext, "you should read files first")
		}
		if len(exec.calls) != 1 {
			t.Fatalf("executor call count = %d, want 1", len(exec.calls))
		}
	})

	t.Run("post-tool-use multiple matches returns first inject response", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{
				makeMatch("log-only", types.ActionLog, "just logging"),
				makeMatch("inject-first", types.ActionInject, "first inject"),
			},
		}
		callCount := 0
		customExec := &mockExecutorFunc{
			fn: func(match types.RuleMatch) (*types.HookResponse, error) {
				callCount++
				if match.Rule.Action.Type == types.ActionInject {
					return &types.HookResponse{AdditionalContext: "injected context"}, nil
				}
				return nil, nil
			},
		}
		ts, _ := startTestServer(t, eval, customExec)
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-inject-2", "PostToolUse", "Edit"))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.AdditionalContext != "injected context" {
			t.Errorf("additionalContext = %q, want %q", body.AdditionalContext, "injected context")
		}
		if callCount != 2 {
			t.Errorf("executor was called %d times, want 2", callCount)
		}
	})

	t.Run("post-tool-use no inject returns empty response", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("log-match", types.ActionLog, "logged")},
		}
		exec := &mockExecutor{resp: nil}
		ts, _ := startTestServer(t, eval, exec)
		resp := postJSON(t, ts, "/hooks/post-tool-use", makeEvent("sess-inject-3", "PostToolUse", "Edit"))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
		if body.AdditionalContext != "" {
			t.Errorf("additionalContext = %q, want empty", body.AdditionalContext)
		}
	})

	t.Run("pre-tool-use block falls back when executor errors", func(t *testing.T) {
		eval := &mockEvaluator{
			matches: []types.RuleMatch{makeMatch("block-on-error", types.ActionBlock, "fallback reason")},
		}
		exec := &mockExecutor{err: fmt.Errorf("executor failed")}
		ts, _ := startTestServer(t, eval, exec)
		resp := postJSON(t, ts, "/hooks/pre-tool-use", makeEvent("sess-exec-3", "PreToolUse", "Bash"))
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "block" {
			t.Errorf("decision = %q, want %q", body.Decision, "block")
		}
		if body.Reason != "fallback reason" {
			t.Errorf("reason = %q, want %q", body.Reason, "fallback reason")
		}
	})
}

// --- Reload Rules Tests ---

func startReloadTestServer(t *testing.T, rulesDir string) (*httptest.Server, *mockEvaluator) {
	t.Helper()
	eval := &mockEvaluator{}
	tracker := NewTracker(10 * time.Minute)
	srv := NewServer(":0", rulesDir, tracker, eval, nil, "")
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, eval
}

func TestReloadRules(t *testing.T) {
	t.Run("reload with valid rulesDir", func(t *testing.T) {
		dir := t.TempDir()
		ruleYAML := `rules:
  - name: test-reload-rule
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
          count: 1
    action:
      type: log
      message: test
`
		if err := os.WriteFile(dir+"/reload.yaml", []byte(ruleYAML), 0o644); err != nil {
			t.Fatalf("failed to write rule file: %v", err)
		}

		ts, eval := startReloadTestServer(t, dir)

		resp, err := ts.Client().Post(ts.URL+"/admin/reload-rules", "", nil)
		if err != nil {
			t.Fatalf("POST /admin/reload-rules failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if len(eval.replaced) != 1 {
			t.Errorf("eval.replaced count = %d, want 1", len(eval.replaced))
		}
		if len(eval.replaced) > 0 && eval.replaced[0].Name != "test-reload-rule" {
			t.Errorf("rule name = %q, want %q", eval.replaced[0].Name, "test-reload-rule")
		}
	})

	t.Run("reload with no rulesDir returns 400", func(t *testing.T) {
		ts, _ := startReloadTestServer(t, "")

		resp, err := ts.Client().Post(ts.URL+"/admin/reload-rules", "", nil)
		if err != nil {
			t.Fatalf("POST /admin/reload-rules failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("reload with invalid rulesDir returns 500", func(t *testing.T) {
		ts, _ := startReloadTestServer(t, "/nonexistent/path")

		resp, err := ts.Client().Post(ts.URL+"/admin/reload-rules", "", nil)
		if err != nil {
			t.Fatalf("POST /admin/reload-rules failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
		}
	})
}

// --- Admin Auth Tests ---

func startAuthTestServer(t *testing.T, rulesDir, token string) *httptest.Server {
	t.Helper()
	eval := &mockEvaluator{}
	tracker := NewTracker(10 * time.Minute)
	srv := NewServer(":0", rulesDir, tracker, eval, nil, token)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func TestAdminAuth(t *testing.T) {
	t.Run("returns 401 without token", func(t *testing.T) {
		dir := t.TempDir()
		ts := startAuthTestServer(t, dir, "secret-token-123")

		resp, err := ts.Client().Post(ts.URL+"/admin/reload-rules", "", nil)
		if err != nil {
			t.Fatalf("POST /admin/reload-rules failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
		body := decodeJSON[map[string]string](t, resp)
		if body["error"] != "unauthorized" {
			t.Errorf("error = %q, want %q", body["error"], "unauthorized")
		}
	})

	t.Run("returns 401 with wrong token", func(t *testing.T) {
		dir := t.TempDir()
		ts := startAuthTestServer(t, dir, "secret-token-123")

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/reload-rules", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer wrong-token")

		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("succeeds with correct token", func(t *testing.T) {
		dir := t.TempDir()
		ruleYAML := `rules:
  - name: auth-test-rule
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
          count: 1
    action:
      type: log
      message: test
`
		if err := os.WriteFile(dir+"/rule.yaml", []byte(ruleYAML), 0o644); err != nil {
			t.Fatalf("failed to write rule file: %v", err)
		}

		token := "secret-token-123"
		ts := startAuthTestServer(t, dir, token)

		req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/reload-rules", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("no token configured allows unauthenticated access", func(t *testing.T) {
		dir := t.TempDir()
		ruleYAML := `rules:
  - name: noauth-rule
    enabled: true
    trigger:
      conditions:
        - event: PostToolUse
          count: 1
    action:
      type: log
      message: test
`
		if err := os.WriteFile(dir+"/rule.yaml", []byte(ruleYAML), 0o644); err != nil {
			t.Fatalf("failed to write rule file: %v", err)
		}

		ts := startAuthTestServer(t, dir, "")

		resp, err := ts.Client().Post(ts.URL+"/admin/reload-rules", "", nil)
		if err != nil {
			t.Fatalf("POST /admin/reload-rules failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d (backward compat)", resp.StatusCode, http.StatusOK)
		}
	})
}

// --- Admin Token File Tests ---

func TestAdminTokenFile(t *testing.T) {
	t.Run("generate and read round-trip", func(t *testing.T) {
		dir := t.TempDir()
		token, err := GenerateAdminToken(dir)
		if err != nil {
			t.Fatalf("GenerateAdminToken failed: %v", err)
		}
		if len(token) != 64 { // 32 bytes = 64 hex chars
			t.Errorf("token length = %d, want 64", len(token))
		}

		got := ReadAdminToken(dir)
		if got != token {
			t.Errorf("ReadAdminToken = %q, want %q", got, token)
		}
	})

	t.Run("read returns empty for missing file", func(t *testing.T) {
		dir := t.TempDir()
		got := ReadAdminToken(dir)
		if got != "" {
			t.Errorf("ReadAdminToken = %q, want empty string", got)
		}
	})

	t.Run("generate creates file with restricted permissions", func(t *testing.T) {
		dir := t.TempDir()
		_, err := GenerateAdminToken(dir)
		if err != nil {
			t.Fatalf("GenerateAdminToken failed: %v", err)
		}

		info, err := os.Stat(dir + "/admin.token")
		if err != nil {
			t.Fatalf("stat admin.token: %v", err)
		}
		perm := info.Mode().Perm()
		if perm != 0o600 {
			t.Errorf("file permissions = %o, want 600", perm)
		}
	})
}

// --- Tracker Tests ---

func TestTracker(t *testing.T) {
	t.Run("records and retrieves activities", func(t *testing.T) {
		tracker := NewTracker(10 * time.Minute)
		tracker.Record(makeEvent("s1", "PostToolUse", "Bash"))

		activities := tracker.GetActivities("s1")
		if len(activities) != 1 {
			t.Fatalf("count = %d, want 1", len(activities))
		}
		if activities[0].Event.ToolName != "Bash" {
			t.Errorf("tool = %q, want %q", activities[0].Event.ToolName, "Bash")
		}
	})

	t.Run("filters by session", func(t *testing.T) {
		tracker := NewTracker(10 * time.Minute)
		tracker.Record(makeEvent("s1", "", ""))
		tracker.Record(makeEvent("s2", "", ""))

		if len(tracker.GetActivities("s1")) != 1 {
			t.Error("expected 1 activity for s1")
		}
		if len(tracker.GetActivities("s2")) != 1 {
			t.Error("expected 1 activity for s2")
		}
		if len(tracker.GetActivities("s3")) != 0 {
			t.Error("expected 0 activities for s3")
		}
	})

	t.Run("cleans up old activities", func(t *testing.T) {
		tracker := NewTracker(5 * time.Minute)

		oldEvent := types.Event{SessionID: "s1", Timestamp: time.Now().Add(-10 * time.Minute)}
		tracker.Record(oldEvent)

		recentEvent := types.Event{SessionID: "s1", Timestamp: time.Now()}
		tracker.Record(recentEvent)

		activities := tracker.GetActivities("s1")
		if len(activities) != 1 {
			t.Fatalf("count = %d, want 1 (old should be cleaned)", len(activities))
		}
	})
}

// --- Tracker Resource Cap Tests ---

func TestTrackerSessionEviction(t *testing.T) {
	t.Run("evicts oldest session when at maxSessions", func(t *testing.T) {
		tracker := NewTracker(1 * time.Hour)
		now := time.Now()

		// Fill up to maxSessions with sessions whose last activity timestamp
		// increases with session index, so session "s-0" is the oldest.
		for i := 0; i < maxSessions; i++ {
			event := types.Event{
				SessionID: fmt.Sprintf("s-%d", i),
				Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			}
			tracker.Record(event)
		}

		counts := tracker.SessionCounts()
		if len(counts) != maxSessions {
			t.Fatalf("session count = %d, want %d", len(counts), maxSessions)
		}

		// Record a new session beyond the limit.
		newEvent := types.Event{
			SessionID: "s-new",
			Timestamp: now.Add(time.Duration(maxSessions) * time.Millisecond),
		}
		tracker.Record(newEvent)

		counts = tracker.SessionCounts()
		if len(counts) != maxSessions {
			t.Fatalf("session count after eviction = %d, want %d", len(counts), maxSessions)
		}

		// The oldest session (s-0) should have been evicted.
		if _, exists := counts["s-0"]; exists {
			t.Error("expected s-0 to be evicted")
		}

		// The new session should exist.
		if _, exists := counts["s-new"]; !exists {
			t.Error("expected s-new to exist")
		}
	})

	t.Run("does not evict when existing session records", func(t *testing.T) {
		tracker := NewTracker(1 * time.Hour)
		now := time.Now()

		// Fill to maxSessions.
		for i := 0; i < maxSessions; i++ {
			event := types.Event{
				SessionID: fmt.Sprintf("s-%d", i),
				Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			}
			tracker.Record(event)
		}

		// Record another event to an existing session — should not evict.
		existingEvent := types.Event{
			SessionID: "s-0",
			Timestamp: now.Add(time.Duration(maxSessions) * time.Millisecond),
		}
		tracker.Record(existingEvent)

		counts := tracker.SessionCounts()
		if len(counts) != maxSessions {
			t.Fatalf("session count = %d, want %d", len(counts), maxSessions)
		}
		if counts["s-0"] != 2 {
			t.Errorf("s-0 count = %d, want 2", counts["s-0"])
		}
	})
}

func TestTrackerPerSessionCap(t *testing.T) {
	t.Run("caps activities per session", func(t *testing.T) {
		tracker := NewTracker(1 * time.Hour)
		now := time.Now()

		// Record more activities than the cap.
		for i := 0; i < maxActivitiesPerSession+100; i++ {
			event := types.Event{
				SessionID: "s1",
				ToolName:  fmt.Sprintf("tool-%d", i),
				Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			}
			tracker.Record(event)
		}

		activities := tracker.GetActivities("s1")
		if len(activities) > maxActivitiesPerSession {
			t.Fatalf("activity count = %d, want <= %d", len(activities), maxActivitiesPerSession)
		}

		// The most recent activities should be preserved (last N).
		last := activities[len(activities)-1]
		expectedTool := fmt.Sprintf("tool-%d", maxActivitiesPerSession+100-1)
		if last.Event.ToolName != expectedTool {
			t.Errorf("last activity tool = %q, want %q", last.Event.ToolName, expectedTool)
		}
	})
}

func TestTrackerCleanupCopiesSlice(t *testing.T) {
	t.Run("trimmed slice is copied to new backing array", func(t *testing.T) {
		tracker := NewTracker(5 * time.Minute)
		now := time.Now()

		// Directly inject activities into the tracker: some old, some recent.
		// This bypasses Record to set up a specific internal state where
		// cleanup will find old entries to trim.
		tracker.mu.Lock()
		tracker.sessions["s1"] = []types.Activity{
			{Event: types.Event{SessionID: "s1", ToolName: "old-1"}, Timestamp: now.Add(-10 * time.Minute)},
			{Event: types.Event{SessionID: "s1", ToolName: "old-2"}, Timestamp: now.Add(-9 * time.Minute)},
			{Event: types.Event{SessionID: "s1", ToolName: "old-3"}, Timestamp: now.Add(-8 * time.Minute)},
			{Event: types.Event{SessionID: "s1", ToolName: "recent-1"}, Timestamp: now.Add(-1 * time.Minute)},
		}
		origCap := cap(tracker.sessions["s1"])
		tracker.mu.Unlock()

		// Record a new event, which triggers cleanup.
		tracker.Record(types.Event{
			SessionID: "s1",
			ToolName:  "recent-2",
			Timestamp: now,
		})

		tracker.mu.RLock()
		activities := tracker.sessions["s1"]
		gotCap := cap(activities)
		tracker.mu.RUnlock()

		if len(activities) != 2 {
			t.Fatalf("activity count = %d, want 2", len(activities))
		}
		// The new slice should have been copied (cap == len), not a sub-slice
		// of the original (which would have cap >= origCap).
		if gotCap >= origCap {
			t.Errorf("cap = %d (original %d), want smaller than original (slice should be copied)",
				gotCap, origCap)
		}
		if activities[0].Event.ToolName != "recent-1" {
			t.Errorf("first activity = %q, want %q", activities[0].Event.ToolName, "recent-1")
		}
		if activities[1].Event.ToolName != "recent-2" {
			t.Errorf("second activity = %q, want %q", activities[1].Event.ToolName, "recent-2")
		}
	})
}
