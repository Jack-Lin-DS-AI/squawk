package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// mockEvaluator is a test double for RuleEvaluator.
type mockEvaluator struct {
	matches []types.RuleMatch
}

func (m *mockEvaluator) Evaluate(_ []types.Activity, _ types.Event) []types.RuleMatch {
	return m.matches
}

// mockExecutor is a test double for ActionExecutor.
type mockExecutor struct {
	calls []types.RuleMatch
	resp  *types.HookResponse
	err   error
}

func (m *mockExecutor) Execute(match types.RuleMatch) (*types.HookResponse, error) {
	m.calls = append(m.calls, match)
	return m.resp, m.err
}

// mockExecutorFunc is a test double that delegates to a function for per-call control.
type mockExecutorFunc struct {
	fn func(types.RuleMatch) (*types.HookResponse, error)
}

func (m *mockExecutorFunc) Execute(match types.RuleMatch) (*types.HookResponse, error) {
	return m.fn(match)
}

func newTestServer(evaluator RuleEvaluator) (*Server, *Tracker) {
	tracker := NewTracker(10 * time.Minute)
	srv := NewServer(":0", tracker, evaluator, nil)
	return srv, tracker
}

func newTestServerWithExecutor(evaluator RuleEvaluator, executor ActionExecutor) (*Server, *Tracker) {
	tracker := NewTracker(10 * time.Minute)
	srv := NewServer(":0", tracker, evaluator, executor)
	return srv, tracker
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

func TestServer(t *testing.T) {
	t.Run("health endpoint", func(t *testing.T) {
		srv, _ := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		resp, err := ts.Client().Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		body := decodeJSON[map[string]string](t, resp)
		if body["status"] != "ok" {
			t.Errorf("status = %q, want %q", body["status"], "ok")
		}
	})

	t.Run("status endpoint with no sessions", func(t *testing.T) {
		srv, _ := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

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
		srv, tracker := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-1",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
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
		srv, _ := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-2",
			HookEventName: "PreToolUse",
			ToolName:      "Read",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/pre-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "" {
			t.Errorf("decision = %q, want empty (allow)", body.Decision)
		}
	})

	t.Run("pre-tool-use blocks when block rule matches", func(t *testing.T) {
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:        "block-dangerous-tool",
						Description: "Block dangerous tool usage",
						Enabled:     true,
						Action: types.Action{
							Type:    types.ActionBlock,
							Message: "tool usage blocked by policy",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		srv, _ := newTestServer(evaluator)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-3",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "rm -rf /"},
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/pre-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "block" {
			t.Errorf("decision = %q, want %q", body.Decision, "block")
		}
		if body.Reason != "tool usage blocked by policy" {
			t.Errorf("reason = %q, want %q", body.Reason, "tool usage blocked by policy")
		}
	})

	t.Run("generic event handler records activity", func(t *testing.T) {
		srv, tracker := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-4",
			HookEventName: "Notification",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/event", event)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		resp.Body.Close()

		activities := tracker.GetActivities("sess-4")
		if len(activities) != 1 {
			t.Fatalf("activity count = %d, want 1", len(activities))
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		srv, _ := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		resp, err := ts.Client().Post(
			ts.URL+"/hooks/pre-tool-use",
			"application/json",
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
		srv, _ := newTestServer(&mockEvaluator{})
		ts := httptest.NewServer(srv)
		defer ts.Close()

		// Record events for two sessions.
		for _, sid := range []string{"sess-a", "sess-a", "sess-b"} {
			event := types.Event{
				SessionID:     sid,
				HookEventName: "PostToolUse",
				ToolName:      "Read",
				Timestamp:     time.Now(),
			}
			resp := postJSON(t, ts, "/hooks/post-tool-use", event)
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
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "notify-frequent-edits",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionNotify,
							Message: "lots of edits detected",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		srv, _ := newTestServer(evaluator)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-5",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		// PostToolUse should always return an allow response.
		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
	})

	t.Run("post-tool-use calls executor for each match", func(t *testing.T) {
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "notify-edits",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionNotify,
							Message: "edit detected",
						},
					},
					MatchedAt: time.Now(),
				},
				{
					Rule: types.Rule{
						Name:    "log-edits",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionLog,
							Message: "edit logged",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		exec := &mockExecutor{}
		srv, _ := newTestServerWithExecutor(evaluator, exec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-exec-1",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
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
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "block-dangerous",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionBlock,
							Message: "original message",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		exec := &mockExecutor{
			resp: &types.HookResponse{
				Decision: "block",
				Reason:   "expanded by executor",
			},
		}
		srv, _ := newTestServerWithExecutor(evaluator, exec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-exec-2",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput:     map[string]any{"command": "rm -rf /"},
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/pre-tool-use", event)
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
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "inject-guidance",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionInject,
							Message: "you should read files first",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		exec := &mockExecutor{
			resp: &types.HookResponse{
				AdditionalContext: "you should read files first",
			},
		}
		srv, _ := newTestServerWithExecutor(evaluator, exec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-inject-1",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
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
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "log-only",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionLog,
							Message: "just logging",
						},
					},
					MatchedAt: time.Now(),
				},
				{
					Rule: types.Rule{
						Name:    "inject-first",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionInject,
							Message: "first inject",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		callCount := 0
		// Use a custom executor that returns different responses per call.
		customExec := &mockExecutorFunc{
			fn: func(match types.RuleMatch) (*types.HookResponse, error) {
				callCount++
				if match.Rule.Action.Type == types.ActionInject {
					return &types.HookResponse{
						AdditionalContext: "injected context",
					}, nil
				}
				return nil, nil // log action returns nil
			},
		}
		srv, _ := newTestServerWithExecutor(evaluator, customExec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-inject-2",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		if body.AdditionalContext != "injected context" {
			t.Errorf("additionalContext = %q, want %q", body.AdditionalContext, "injected context")
		}
		if callCount != 2 {
			t.Errorf("executor was called %d times, want 2 (should execute all matches)", callCount)
		}
	})

	t.Run("post-tool-use no inject returns empty response", func(t *testing.T) {
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "log-match",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionLog,
							Message: "logged",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		exec := &mockExecutor{resp: nil} // log returns nil
		srv, _ := newTestServerWithExecutor(evaluator, exec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-inject-3",
			HookEventName: "PostToolUse",
			ToolName:      "Edit",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/post-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "" {
			t.Errorf("decision = %q, want empty", body.Decision)
		}
		if body.AdditionalContext != "" {
			t.Errorf("additionalContext = %q, want empty", body.AdditionalContext)
		}
	})

	t.Run("pre-tool-use block falls back when executor errors", func(t *testing.T) {
		evaluator := &mockEvaluator{
			matches: []types.RuleMatch{
				{
					Rule: types.Rule{
						Name:    "block-on-error",
						Enabled: true,
						Action: types.Action{
							Type:    types.ActionBlock,
							Message: "fallback reason",
						},
					},
					MatchedAt: time.Now(),
				},
			},
		}

		exec := &mockExecutor{
			err: fmt.Errorf("executor failed"),
		}
		srv, _ := newTestServerWithExecutor(evaluator, exec)
		ts := httptest.NewServer(srv)
		defer ts.Close()

		event := types.Event{
			SessionID:     "sess-exec-3",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			Timestamp:     time.Now(),
		}

		resp := postJSON(t, ts, "/hooks/pre-tool-use", event)
		body := decodeJSON[types.HookResponse](t, resp)

		if body.Decision != "block" {
			t.Errorf("decision = %q, want %q", body.Decision, "block")
		}
		if body.Reason != "fallback reason" {
			t.Errorf("reason = %q, want %q", body.Reason, "fallback reason")
		}
	})
}

func TestTracker(t *testing.T) {
	t.Run("records and retrieves activities", func(t *testing.T) {
		tracker := NewTracker(10 * time.Minute)

		event := types.Event{
			SessionID:     "s1",
			HookEventName: "PostToolUse",
			ToolName:      "Bash",
			Timestamp:     time.Now(),
		}
		tracker.Record(event)

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

		tracker.Record(types.Event{SessionID: "s1", Timestamp: time.Now()})
		tracker.Record(types.Event{SessionID: "s2", Timestamp: time.Now()})

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

		// Record an old event.
		oldEvent := types.Event{
			SessionID: "s1",
			Timestamp: time.Now().Add(-10 * time.Minute),
		}
		tracker.Record(oldEvent)

		// Record a recent event which triggers cleanup.
		recentEvent := types.Event{
			SessionID: "s1",
			Timestamp: time.Now(),
		}
		tracker.Record(recentEvent)

		activities := tracker.GetActivities("s1")
		if len(activities) != 1 {
			t.Fatalf("count = %d, want 1 (old should be cleaned)", len(activities))
		}
	})

	t.Run("GetActivitiesSince filters by time", func(t *testing.T) {
		tracker := NewTracker(1 * time.Hour)

		now := time.Now()
		tracker.Record(types.Event{SessionID: "s1", ToolName: "A", Timestamp: now.Add(-30 * time.Minute)})
		tracker.Record(types.Event{SessionID: "s1", ToolName: "B", Timestamp: now.Add(-10 * time.Minute)})
		tracker.Record(types.Event{SessionID: "s1", ToolName: "C", Timestamp: now})

		activities := tracker.GetActivitiesSince("s1", now.Add(-15*time.Minute))
		if len(activities) != 2 {
			t.Fatalf("count = %d, want 2", len(activities))
		}
		if activities[0].Event.ToolName != "B" {
			t.Errorf("first = %q, want %q", activities[0].Event.ToolName, "B")
		}
		if activities[1].Event.ToolName != "C" {
			t.Errorf("second = %q, want %q", activities[1].Event.ToolName, "C")
		}
	})
}
