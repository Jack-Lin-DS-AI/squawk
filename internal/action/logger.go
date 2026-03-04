package action

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Jack-Lin-DS-AI/squawk/internal/types"
)

// LogEntry represents a single entry in the action log file.
type LogEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	RuleName      string    `json:"rule_name"`
	Action        string    `json:"action"`
	Message       string    `json:"message"`
	SessionID     string    `json:"session_id,omitempty"`
	Project       string    `json:"project,omitempty"`
	ActivityCount int       `json:"activity_count,omitempty"`
	ToolName      string    `json:"tool_name,omitempty"`
	FilePath      string    `json:"file_path,omitempty"`
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
	entry := LogEntry{
		Timestamp:     match.MatchedAt,
		RuleName:      match.Rule.Name,
		Action:        string(match.Rule.Action.Type),
		Message:       msg,
		ActivityCount: len(match.Activities),
	}
	enrichFromMatch(&entry, match)
	l.write(entry)
}

// enrichFromMatch populates session, project, tool, and file fields from the
// match's activities. It uses the first activity for tool/file context and
// derives the project from the event's working directory.
func enrichFromMatch(entry *LogEntry, match types.RuleMatch) {
	if len(match.Activities) == 0 {
		return
	}
	first := match.Activities[0]
	entry.SessionID = first.SessionID
	entry.Project = first.Event.CWD
	entry.ToolName = first.Event.ToolName
	if fp, ok := first.Event.ToolInput["file_path"].(string); ok {
		entry.FilePath = fp
	}
}

// GetRecentLogs returns the last n log entries from the log file. If fewer
// than n entries exist, all entries are returned. The entries are returned
// in chronological order (oldest first).
func (l *ActionLogger) GetRecentLogs(n int) ([]LogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return ReadLogEntries(l.logFile, n)
}

// ReadLogEntries reads entries from a JSON-lines log file. If n > 0, only
// the last n entries are returned using a ring buffer to bound memory.
// If n <= 0, all entries are returned. Returns nil, nil if the file does
// not exist.
func ReadLogEntries(logFile string, n int) ([]LogEntry, error) {
	f, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open log file %q: %w", logFile, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// When n > 0, use a ring buffer to keep only the last n entries.
	if n > 0 {
		ring := make([]LogEntry, n)
		count := 0
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var entry LogEntry
			if err := json.Unmarshal(line, &entry); err != nil {
				continue
			}
			ring[count%n] = entry
			count++
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read log file: %w", err)
		}
		if count == 0 {
			return nil, nil
		}
		if count <= n {
			return ring[:count], nil
		}
		// Reorder ring to chronological order.
		start := count % n
		result := make([]LogEntry, n)
		copy(result, ring[start:])
		copy(result[n-start:], ring[:start])
		return result, nil
	}

	// n <= 0: return all entries.
	var all []LogEntry
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		all = append(all, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}
	return all, nil
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
	if err := l.encoder.Encode(entry); err != nil {
		log.Printf("squawk: failed to write action log entry: %v", err)
	}
}
