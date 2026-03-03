// Package types defines shared types used across all squawk packages.
package types

import "time"

// Event represents a Claude Code hook event received by squawk.
type Event struct {
	SessionID     string         `json:"session_id"`
	CWD           string         `json:"cwd"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	ToolOutput    string         `json:"tool_output,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
}

// Rule defines a supervision rule with trigger conditions and actions.
type Rule struct {
	Name        string    `yaml:"name" json:"name"`
	Description string    `yaml:"description" json:"description"`
	Enabled     bool      `yaml:"enabled" json:"enabled"`
	Trigger     Trigger   `yaml:"trigger" json:"trigger"`
	Action      Action    `yaml:"action" json:"action"`
	Priority    int       `yaml:"priority" json:"priority"`
}

// Trigger defines when a rule should fire.
type Trigger struct {
	Conditions []Condition `yaml:"conditions" json:"conditions"`
	Logic      string      `yaml:"logic" json:"logic"` // "and" or "or", default "and"
}

// Condition defines a single condition within a trigger.
type Condition struct {
	Event              string `yaml:"event" json:"event"`                               // Hook event name: PreToolUse, PostToolUse, etc.
	Tool               string `yaml:"tool" json:"tool"`                                  // Tool name regex: "Edit|Write"
	FilePattern        string `yaml:"file_pattern" json:"file_pattern"`                  // Glob pattern: match files (e.g. "*_test.go")
	FilePatternExclude string `yaml:"file_pattern_exclude" json:"file_pattern_exclude"`  // Glob pattern: exclude files (match anything NOT matching this)
	Count              int    `yaml:"count" json:"count"`                                // Number of occurrences to trigger
	Within             string `yaml:"within" json:"within"`                              // Time window: "5m", "10m"
	Negate             bool   `yaml:"negate" json:"negate"`                              // Negate this condition
}

// ActionType defines what kind of action to take.
type ActionType string

const (
	ActionBlock  ActionType = "block"
	ActionInject ActionType = "inject"
	ActionNotify ActionType = "notify"
	ActionLog    ActionType = "log"
)

// Action defines what to do when a rule triggers.
type Action struct {
	Type      ActionType `yaml:"type" json:"type"`
	Message   string     `yaml:"message" json:"message"`       // Message to send (block reason, inject prompt, notification text)
	ToolScope string     `yaml:"tool_scope" json:"tool_scope"` // Regex: which tools the block applies to (empty = all)
	FileScope string     `yaml:"file_scope" json:"file_scope"` // Glob: which files the block applies to (empty = all)
	Cooldown  string     `yaml:"cooldown" json:"cooldown"`     // Duration string: "30s", "1m" — suppress re-triggering within this window
}

// Activity represents a tracked tool usage event for pattern detection.
type Activity struct {
	Event     Event     `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
}

// HookResponse is the JSON response sent back to Claude Code hooks.
type HookResponse struct {
	Decision          string `json:"decision,omitempty"`          // "block" or empty (allow)
	Reason            string `json:"reason,omitempty"`
	AdditionalContext string `json:"additionalContext,omitempty"` // Injected context for PostToolUse hooks
}

// RuleMatch represents a triggered rule with context.
type RuleMatch struct {
	Rule       Rule       `json:"rule"`
	Activities []Activity `json:"activities"` // Activities that caused the match
	MatchedAt  time.Time  `json:"matched_at"`
}

// Config holds the squawk configuration.
type Config struct {
	Server    ServerConfig `yaml:"server" json:"server"`
	RulesDir  string       `yaml:"rules_dir" json:"rules_dir"`
	LogFile   string       `yaml:"log_file" json:"log_file"`
	LogLevel  string       `yaml:"log_level" json:"log_level"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
}
