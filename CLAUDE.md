# Squawk

Claude Code 行為監督工具 — 規則驅動的即時偵測 + 主動介入矯正。

## Architecture

```
cmd/squawk/main.go       — CLI entrypoint (cobra)
internal/types/           — Shared types (Event, Rule, Action, Condition)
internal/rules/           — Rule engine: parsing YAML, evaluating conditions
internal/monitor/         — HTTP hook server + activity tracker
internal/action/          — Action executor (block, inject prompt, notify)
internal/config/          — Config management
rules/                    — Default & community rule YAML files
```

## Conventions

- Go 1.26, module: github.com/jacklin/squawk
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

## How It Works

1. `squawk watch` starts HTTP server on localhost
2. Claude Code hooks POST events to squawk
3. squawk records activity + evaluates rules
4. On rule match: returns block decision or queues action
5. PreToolUse hooks can block synchronously via JSON response
6. PostToolUse hooks track patterns asynchronously
