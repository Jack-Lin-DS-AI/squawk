# Squawk

Stateful behavioral pattern detection for AI coding agents.

Squawk monitors Claude Code sessions in real-time, detects cross-event behavioral
patterns (e.g., repeatedly editing tests without reading source), and intervenes
with rule-driven actions (block, inject prompt, notify).

**Key distinction:** Squawk focuses on _cross-event patterns_ that require tracking
state over time — NOT single-event guards like command allowlists or permission checks.

## Architecture

```
cmd/squawk/main.go       — CLI entrypoint (cobra)
internal/types/           — Shared types (Event, Rule, Action, Condition)
internal/rules/           — Rule engine: parsing YAML, evaluating conditions
internal/monitor/         — HTTP hook server + activity tracker
internal/action/          — Action executor (block, inject prompt, notify)
internal/config/          — Config management
rules/                    — Default rule YAML files
docs/                     — Research notes, rules catalog
```

## How It Works

1. `squawk watch` starts HTTP server on localhost
2. Claude Code hooks POST events to squawk
3. squawk records activity + evaluates rules across event history
4. On rule match: returns block decision or queues action
5. PreToolUse hooks can block synchronously via JSON response
6. PostToolUse hooks track patterns asynchronously

## Conventions

- Go 1.26, module: github.com/Jack-Lin-DS-AI/squawk
- gofmt + goimports mandatory
- Accept interfaces, return structs
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`
- Table-driven tests with `-race` flag
- YAML for rule definitions
- CLI framework: cobra

## Key Types (internal/types/)

- `Event` — Claude Code hook event (tool_name, tool_input, etc.)
- `Rule` — Trigger condition + action pair
- `Condition` — What triggers a rule (pattern match, count threshold, time window)
- `Action` — What to do when triggered (block, inject, notify)
- `Activity` — Tracked tool usage history for pattern detection
