# Squawk

Stateful behavioral pattern detection for AI coding agents.

Squawk monitors Claude Code sessions in real-time, detects cross-event behavioral
patterns (e.g., repeatedly editing tests without reading source), and intervenes
with rule-driven actions (block, inject prompt, notify).

**Key distinction:** Squawk focuses on _cross-event patterns_ that require tracking
state over time — NOT single-event guards like command allowlists or permission checks.

## CLI Commands (for Claude Code to run via Bash)

Lifecycle:
- `squawk setup` — one-command bootstrap (init + hooks + daemon)
- `squawk stop` — stop the background daemon
- `squawk teardown` — stop daemon + remove hooks (add --remove-data to also delete .squawk/)
- `squawk status` — show daemon, hooks, and session status
- `squawk init` — manual bootstrap (creates config, prints hooks snippet; prefer `setup`)

Rules:
- `squawk rules list` — show all rules with enabled/disabled status
- `squawk rules enable <name>` — enable a rule (hot-reloads in running server)
- `squawk rules disable <name>` — disable a rule (hot-reloads in running server)
- `squawk rules remove <name> --force` — permanently remove a rule
- `squawk rules add` — interactive rule creation
- `squawk rules test --scenario <name>` — test rules against simulated events (test-modification, excessive-retry, blind-creation)

Monitoring:
- `squawk stats` — overall intervention metrics
- `squawk stats --since 7d` — metrics for last 7 days
- `squawk stats --project <path>` — per-project metrics
- `squawk stats --json` — machine-readable output
- `squawk log --tail 20` — recent action log entries

## Architecture

```
cmd/squawk/main.go       — CLI entrypoint (cobra)
internal/types/           — Shared types (Event, Rule, Action, Condition)
internal/rules/           — Rule engine: parsing YAML, evaluating conditions, mutations
internal/monitor/         — HTTP hook server + activity tracker + admin API
internal/action/          — Action executor + logging decorator
internal/stats/           — Metrics aggregation + reporting (squawk stats)
internal/config/          — Config management + settings.json hooks install/uninstall
internal/daemon/          — PID file management + process daemonization
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
7. Every action is logged to `.squawk/squawk.log` (JSON-lines) for metrics
8. `squawk stats` computes aggregated metrics from the action log

## Conventions

- Go 1.25, module: github.com/Jack-Lin-DS-AI/squawk
- Dependencies: cobra, yaml.v3 (no others)
- YAML for rule definitions
- See global rules for Go style, testing, and error handling conventions
