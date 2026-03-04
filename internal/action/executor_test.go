package action

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

func newTestExecutor(t *testing.T) *Executor {
	t.Helper()
	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	return NewExecutor(logger)
}

func newTestActionLogger(t *testing.T) *ActionLogger {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "actions.jsonl")
	logger, err := NewActionLogger(logFile)
	if err != nil {
		t.Fatalf("failed to create action logger: %v", err)
	}
	t.Cleanup(func() { logger.Close() })
	return logger
}

func makeMatch(actionType types.ActionType, message string, activityCount int) types.RuleMatch {
	activities := make([]types.Activity, activityCount)
	for i := range activities {
		activities[i] = types.Activity{
			Timestamp: time.Now(),
			SessionID: "test-session",
		}
	}
	return types.RuleMatch{
		Rule: types.Rule{
			Name:        "test-rule",
			Description: "a test rule",
			Enabled:     true,
			Action: types.Action{
				Type:    actionType,
				Message: message,
			},
		},
		Activities: activities,
		MatchedAt:  time.Now(),
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name                  string
		actionType            types.ActionType
		message               string
		activityCnt           int
		wantDecision          string
		wantReason            string
		wantAdditionalContext string
		wantNil               bool
	}{
		{
			name:         "block action returns block decision",
			actionType:   types.ActionBlock,
			message:      "blocked: too many edits",
			activityCnt:  1,
			wantDecision: "block",
			wantReason:   "blocked: too many edits",
		},
		{
			name:                  "inject action returns additionalContext without decision",
			actionType:            types.ActionInject,
			message:               "please run tests before continuing",
			activityCnt:           2,
			wantDecision:          "",
			wantAdditionalContext: "please run tests before continuing",
		},
		{
			name:        "notify action returns nil response",
			actionType:  types.ActionNotify,
			message:     "heads up: sensitive file accessed",
			activityCnt: 1,
			wantNil:     true,
		},
		{
			name:        "log action returns nil response",
			actionType:  types.ActionLog,
			message:     "tool usage recorded",
			activityCnt: 3,
			wantNil:     true,
		},
		{
			name:         "template replaces count in block message",
			actionType:   types.ActionBlock,
			message:      "detected {count} edits without tests",
			activityCnt:  7,
			wantDecision: "block",
			wantReason:   "detected 7 edits without tests",
		},
		{
			name:                  "template replaces count in inject message",
			actionType:            types.ActionInject,
			message:               "{count} file writes detected",
			activityCnt:           12,
			wantDecision:          "",
			wantAdditionalContext: "12 file writes detected",
		},
		{
			name:         "zero activities replaces count with 0",
			actionType:   types.ActionBlock,
			message:      "{count} occurrences",
			activityCnt:  0,
			wantDecision: "block",
			wantReason:   "0 occurrences",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := newTestExecutor(t)
			match := makeMatch(tt.actionType, tt.message, tt.activityCnt)

			resp, err := executor.Execute(match)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if resp != nil {
					t.Fatalf("expected nil response, got %+v", resp)
				}
				return
			}

			if resp == nil {
				t.Fatal("expected non-nil response, got nil")
			}
			if resp.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q", resp.Decision, tt.wantDecision)
			}
			if resp.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", resp.Reason, tt.wantReason)
			}
			if resp.AdditionalContext != tt.wantAdditionalContext {
				t.Errorf("additionalContext = %q, want %q", resp.AdditionalContext, tt.wantAdditionalContext)
			}
		})
	}
}

func TestExecuteUnsupportedAction(t *testing.T) {
	executor := newTestExecutor(t)
	match := makeMatch("unknown", "should fail", 1)

	_, err := executor.Execute(match)
	if err == nil {
		t.Fatal("expected error for unsupported action type, got nil")
	}
}

func TestExpandTemplateFileAndCommand(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		toolInput  map[string]any
		wantResult string
	}{
		{
			name:       "replaces file from tool_input",
			message:    "editing {file}",
			toolInput:  map[string]any{"file_path": "/src/main.go"},
			wantResult: "editing /src/main.go",
		},
		{
			name:       "replaces command from tool_input",
			message:    "ran: {command}",
			toolInput:  map[string]any{"command": "go test ./..."},
			wantResult: "ran: go test ./...",
		},
		{
			name:       "replaces all three variables",
			message:    "{count} edits to {file} after running {command}",
			toolInput:  map[string]any{"file_path": "/a/b.go", "command": "make build"},
			wantResult: "2 edits to /a/b.go after running make build",
		},
		{
			name:       "missing file_path replaces with empty",
			message:    "file={file} cmd={command}",
			toolInput:  map[string]any{"command": "ls"},
			wantResult: "file= cmd=ls",
		},
		{
			name:       "missing command replaces with empty",
			message:    "file={file} cmd={command}",
			toolInput:  map[string]any{"file_path": "/a.go"},
			wantResult: "file=/a.go cmd=",
		},
		{
			name:       "no activities replaces all with empty or zero",
			message:    "{count} {file} {command}",
			toolInput:  nil,
			wantResult: "0  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var activities []types.Activity
			if tt.toolInput != nil {
				activities = []types.Activity{
					{
						Event: types.Event{
							ToolInput: tt.toolInput,
						},
						Timestamp: time.Now(),
						SessionID: "test-session",
					},
					{
						Event:     types.Event{},
						Timestamp: time.Now(),
						SessionID: "test-session",
					},
				}
			}
			match := types.RuleMatch{
				Rule: types.Rule{
					Name:    "template-test",
					Enabled: true,
					Action: types.Action{
						Type:    types.ActionBlock,
						Message: tt.message,
					},
				},
				Activities: activities,
				MatchedAt:  time.Now(),
			}
			got := expandTemplate(tt.message, match)
			if got != tt.wantResult {
				t.Errorf("expandTemplate() = %q, want %q", got, tt.wantResult)
			}
		})
	}
}

func TestLoggingExecutor(t *testing.T) {
	t.Run("delegates to inner executor and logs action", func(t *testing.T) {
		actionLogger := newTestActionLogger(t)
		le := NewLoggingExecutor(newTestExecutor(t), actionLogger)

		match := types.RuleMatch{
			Rule: types.Rule{
				Name:    "test-logging",
				Enabled: true,
				Action:  types.Action{Type: types.ActionBlock, Message: "blocked"},
			},
			Activities: []types.Activity{
				{
					Event: types.Event{
						SessionID: "sess-1",
						CWD:       "/Users/jacklin/Projects/cozydrop",
						ToolName:  "Edit",
						ToolInput: map[string]any{"file_path": "/src/main.go"},
					},
					Timestamp: time.Now(),
					SessionID: "sess-1",
				},
				{
					Event:     types.Event{SessionID: "sess-1"},
					Timestamp: time.Now(),
					SessionID: "sess-1",
				},
			},
			MatchedAt: time.Now(),
		}

		resp, err := le.Execute(match)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || resp.Decision != "block" {
			t.Fatalf("expected block response, got %+v", resp)
		}

		entries, err := actionLogger.GetRecentLogs(10)
		if err != nil {
			t.Fatalf("failed to get recent logs: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(entries))
		}
		e := entries[0]
		if e.RuleName != "test-logging" {
			t.Errorf("rule_name = %q, want %q", e.RuleName, "test-logging")
		}
		if e.SessionID != "sess-1" {
			t.Errorf("session_id = %q, want %q", e.SessionID, "sess-1")
		}
		if e.Project != "/Users/jacklin/Projects/cozydrop" {
			t.Errorf("project = %q, want %q", e.Project, "/Users/jacklin/Projects/cozydrop")
		}
		if e.ActivityCount != 2 {
			t.Errorf("activity_count = %d, want %d", e.ActivityCount, 2)
		}
		if e.ToolName != "Edit" {
			t.Errorf("tool_name = %q, want %q", e.ToolName, "Edit")
		}
		if e.FilePath != "/src/main.go" {
			t.Errorf("file_path = %q, want %q", e.FilePath, "/src/main.go")
		}
	})

	t.Run("propagates executor errors without logging", func(t *testing.T) {
		actionLogger := newTestActionLogger(t)
		le := NewLoggingExecutor(newTestExecutor(t), actionLogger)

		match := makeMatch("unknown", "should fail", 1)
		_, err := le.Execute(match)
		if err == nil {
			t.Fatal("expected error for unsupported action type, got nil")
		}

		entries, err := actionLogger.GetRecentLogs(10)
		if err != nil {
			t.Fatalf("failed to get recent logs: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected 0 log entries on error, got %d", len(entries))
		}
	})
}

func TestActionLogger_LogAction(t *testing.T) {
	tests := []struct {
		name    string
		rule    types.Rule
		resp    *types.HookResponse
		wantMsg string
	}{
		{
			name: "uses AdditionalContext from response",
			rule: types.Rule{Name: "inject-rule", Enabled: true,
				Action: types.Action{Type: types.ActionInject, Message: "original message"}},
			resp:    &types.HookResponse{AdditionalContext: "expanded inject message"},
			wantMsg: "expanded inject message",
		},
		{
			name: "uses Reason when AdditionalContext is empty",
			rule: types.Rule{Name: "block-rule", Enabled: true,
				Action: types.Action{Type: types.ActionBlock, Message: "original"}},
			resp:    &types.HookResponse{Decision: "block", Reason: "expanded block reason"},
			wantMsg: "expanded block reason",
		},
		{
			name: "nil response uses action message",
			rule: types.Rule{Name: "log-rule", Enabled: true,
				Action: types.Action{Type: types.ActionLog, Message: "the original message"}},
			resp:    nil,
			wantMsg: "the original message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := newTestActionLogger(t)
			match := types.RuleMatch{
				Rule:       tt.rule,
				Activities: []types.Activity{{Timestamp: time.Now(), SessionID: "s1"}},
				MatchedAt:  time.Now(),
			}
			logger.LogAction(match, tt.resp)

			entries, err := logger.GetRecentLogs(10)
			if err != nil {
				t.Fatalf("failed to get recent logs: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", entries[0].Message, tt.wantMsg)
			}
		})
	}
}

func TestActionLogger_LogDaemonStart(t *testing.T) {
	logger := newTestActionLogger(t)
	logger.LogDaemonStart()

	entries, err := logger.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("failed to get recent logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != DaemonStartAction {
		t.Errorf("action = %q, want %q", e.Action, DaemonStartAction)
	}
	if e.Message != "Squawk daemon started" {
		t.Errorf("message = %q, want %q", e.Message, "Squawk daemon started")
	}
	if e.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestActionLogger_GetRecentLogs(t *testing.T) {
	logger := newTestActionLogger(t)

	for i := 0; i < 5; i++ {
		logger.LogAction(types.RuleMatch{
			Rule: types.Rule{
				Name:   fmt.Sprintf("rule-%d", i),
				Action: types.Action{Type: types.ActionLog, Message: "msg"},
			},
			MatchedAt: time.Now(),
		}, nil)
	}

	entries, err := logger.GetRecentLogs(3)
	if err != nil {
		t.Fatalf("failed to get recent logs: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].RuleName != "rule-2" {
		t.Errorf("first entry = %q, want %q", entries[0].RuleName, "rule-2")
	}
}
