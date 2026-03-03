// Package action provides the action executor that runs actions (block, inject,
// notify, log) in response to rule matches detected by the rule engine.
package action

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jacklin/squawk/internal/types"
)

// Executor executes actions based on rule matches.
type Executor struct {
	logger *log.Logger
}

// NewExecutor creates a new action executor with the given logger.
func NewExecutor(logger *log.Logger) *Executor {
	return &Executor{logger: logger}
}

// Execute runs the action associated with the given rule match and returns
// the appropriate HookResponse. For block actions the response instructs
// Claude Code to reject the tool call. For inject actions the message is
// surfaced to Claude Code via hook output. Notify and log actions produce
// side-effects only and return a nil response.
func (e *Executor) Execute(match types.RuleMatch) (*types.HookResponse, error) {
	msg := expandTemplate(match.Rule.Action.Message, match)

	switch match.Rule.Action.Type {
	case types.ActionBlock:
		e.logger.Printf("[BLOCK] rule=%q message=%q", match.Rule.Name, msg)
		return &types.HookResponse{
			Decision: "block",
			Reason:   msg,
		}, nil

	case types.ActionInject:
		e.logger.Printf("[INJECT] rule=%q message=%q", match.Rule.Name, msg)
		return &types.HookResponse{
			Reason: msg,
		}, nil

	case types.ActionNotify:
		e.logger.Printf("[NOTIFY] rule=%q message=%q", match.Rule.Name, msg)
		sendNotification(match.Rule.Name, msg)
		return nil, nil

	case types.ActionLog:
		e.logger.Printf("[LOG] rule=%q message=%q", match.Rule.Name, msg)
		return nil, nil

	default:
		return nil, fmt.Errorf("unsupported action type: %q", match.Rule.Action.Type)
	}
}

// expandTemplate replaces template variables in the message string with
// values from the rule match context. Supported variables:
//
//	{count} — number of activities that triggered the match
func expandTemplate(message string, match types.RuleMatch) string {
	count := strconv.Itoa(len(match.Activities))
	return strings.ReplaceAll(message, "{count}", count)
}

// sendNotification attempts to send a macOS desktop notification using
// osascript. Failures are silently ignored because notifications are
// best-effort and must never block the hook response.
func sendNotification(title, message string) {
	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	_ = exec.Command("osascript", "-e", script).Run()
}
