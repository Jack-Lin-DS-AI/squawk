package action

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jacklin/squawk/internal/types"
)

// LogEntry represents a single entry in the action log file.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	RuleName  string    `json:"rule_name"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
}

// ActionLogger writes rule match and action events to a JSON-lines log file.
type ActionLogger struct {
	mu      sync.Mutex
	logFile string
	file    *os.File
	encoder *json.Encoder
}

// NewActionLogger creates an ActionLogger that appends to the given file path.
// The file is created if it does not exist.
func NewActionLogger(logFile string) (*ActionLogger, error) {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open action log file %q: %w", logFile, err)
	}
	return &ActionLogger{
		logFile: logFile,
		file:    f,
		encoder: json.NewEncoder(f),
	}, nil
}

// LogMatch records a rule match event to the log file.
func (l *ActionLogger) LogMatch(match types.RuleMatch) {
	l.write(LogEntry{
		Timestamp: match.MatchedAt,
		RuleName:  match.Rule.Name,
		Action:    string(match.Rule.Action.Type),
		Message:   fmt.Sprintf("rule matched with %d activities", len(match.Activities)),
	})
}

// LogAction records the action taken for a rule match along with the response
// returned to Claude Code.
func (l *ActionLogger) LogAction(match types.RuleMatch, response *types.HookResponse) {
	msg := match.Rule.Action.Message
	if response != nil && response.Reason != "" {
		msg = response.Reason
	}
	if response != nil && response.AdditionalContext != "" {
		msg = response.AdditionalContext
	}
	l.write(LogEntry{
		Timestamp: match.MatchedAt,
		RuleName:  match.Rule.Name,
		Action:    string(match.Rule.Action.Type),
		Message:   msg,
	})
}

// GetRecentLogs returns the last n log entries from the log file. If fewer
// than n entries exist, all entries are returned. The entries are returned
// in chronological order (oldest first).
func (l *ActionLogger) GetRecentLogs(n int) ([]LogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.logFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file for reading: %w", err)
	}
	defer f.Close()

	var all []LogEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		all = append(all, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan log file: %w", err)
	}

	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// Close closes the underlying log file.
func (l *ActionLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// write appends a single LogEntry as a JSON line to the log file.
func (l *ActionLogger) write(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.encoder.Encode(entry)
}
