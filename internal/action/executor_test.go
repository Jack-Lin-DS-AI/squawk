package action

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/jacklin/squawk/internal/types"
)

func newTestExecutor(t *testing.T) *Executor {
	t.Helper()
	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	return NewExecutor(logger)
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
		name         string
		actionType   types.ActionType
		message      string
		activityCnt  int
		wantDecision string
		wantReason   string
		wantNil      bool
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
			name:         "inject action returns reason without decision",
			actionType:   types.ActionInject,
			message:      "please run tests before continuing",
			activityCnt:  2,
			wantDecision: "",
			wantReason:   "please run tests before continuing",
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
			name:         "template replaces count in inject message",
			actionType:   types.ActionInject,
			message:      "{count} file writes detected",
			activityCnt:  12,
			wantDecision: "",
			wantReason:   "12 file writes detected",
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
