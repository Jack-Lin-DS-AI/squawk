# Squawk

Stateful behavior supervision for AI coding agents.

Squawk is a lightweight daemon that monitors [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
tool usage via hooks and detects behavioral anti-patterns that emerge across
multiple actions over time -- loops, oscillation, escalating bad habits --
that no single-event hook can see.

## The Problem

AI coding agents get stuck. They retry the same failing command, weaken tests
instead of fixing code, or delete more code than they add. These patterns
unfold across many tool calls over minutes, not in a single event.

Stateless hooks check each tool call in isolation. They can block `rm -rf` or
protect `.env` files, but they cannot detect that an agent has edited the same
test file five times without ever reading the source code. That requires
memory.

Squawk keeps a sliding window of recent activity and evaluates rules against
the full history. It is a behavioral analyst, not a doorman.

## How It Works

```
    Claude Code session
    |
    |  PreToolUse / PostToolUse hooks
    |  (configured via settings.json)
    v
  curl -s -X POST http://localhost:3131/hooks/...
    |
    v
  +-------------------------------------------+
  |              squawk watch                  |
  |                                            |
  |   Activity Tracker                         |
  |   [event1, event2, event3, ...]            |
  |         |                                  |
  |         v                                  |
  |   Rule Engine                              |
  |   - count events in time window            |
  |   - correlate across event types           |
  |   - detect absence of expected actions     |
  |         |                                  |
  |         v                                  |
  |   Action: block / inject / notify          |
  +-------------------------------------------+
    |
    v
  JSON response to Claude Code
  {"decision":"block","reason":"..."}
```

PreToolUse hooks can block the next action synchronously. PostToolUse hooks
track patterns asynchronously for future evaluation.

## Quick Start

### Install

```bash
go install github.com/Jack-Lin-DS-AI/squawk/cmd/squawk@latest
```

### Initialize

```bash
cd your-project
squawk init
```

This creates `.squawk/config.yaml` and prints a hooks snippet to add to your
Claude Code `~/.claude/settings.json`.

### Start watching

```bash
squawk watch
```

Squawk starts an HTTP server on `localhost:3131`, loads rules from `./rules/`,
and begins evaluating incoming hook events.

### Verify

```bash
# In another terminal:
squawk status
squawk rules list
squawk log --tail 20
```

### Test rules without a live session

```bash
squawk rules test --scenario test-modification
squawk rules test --scenario excessive-retry
squawk rules test --scenario blind-creation
```

## Built-in Rules

Squawk ships with three rules enabled out of the box in `rules/default.yaml`:

| Rule | Trigger | Action |
|------|---------|--------|
| `test-only-modification` | 3+ test file edits in 5 min with zero source reads or edits | block |
| `excessive-retry-same-command` | Same command fails 3+ times in 3 min | block |
| `blind-file-creation` | 3+ new files created in 5 min with zero reads | inject |

Each rule uses AND-logic across multiple conditions, correlating event counts
and absence of expected actions within a sliding time window.

## Rule Catalog

The full roadmap of 12 rules is documented in [docs/RULES_CATALOG.md](docs/RULES_CATALOG.md),
covering:

- **edit-oscillation** -- A-B-A code reversion detection (content hashing)
- **repeated-identical-edit** -- same old/new transformation applied repeatedly
- **test-assertion-weakening** -- progressive weakening of test assertions
- **error-handling-removal** -- stripping error handling over time
- **session-context-warning** -- long session drift notification

Every rule in the catalog requires state that stateless hooks cannot provide.

## Writing Custom Rules

Create a YAML file in `rules/` (or use `squawk rules add`):

```yaml
rules:
  - name: my-custom-rule
    description: "Detect excessive edits to config files"
    enabled: true
    priority: 5
    trigger:
      logic: and
      conditions:
        - event: PostToolUse
          tool: "Edit|Write"
          file_pattern: "*.yaml|*.toml|*.json"
          count: 5
          within: "10m"
    action:
      type: inject
      message: |
        You have modified configuration files {count} times.
        Please verify these changes are intentional and consistent.
```

### Condition fields

| Field | Description |
|-------|-------------|
| `event` | Hook event: `PreToolUse`, `PostToolUse`, `PostToolUseFailure` |
| `tool` | Tool name regex: `"Edit\|Write"`, `"Bash"`, `"Read\|Glob\|Grep"` |
| `file_pattern` | Glob for matching files: `"*_test.go"`, `"*.ts"` |
| `file_pattern_exclude` | Glob for excluding files |
| `count` | Number of occurrences required to trigger |
| `within` | Time window: `"3m"`, `"5m"`, `"10m"` |
| `negate` | Invert the condition (true = require absence) |

### Action types

| Type | Behavior |
|------|----------|
| `block` | Return `{"decision":"block"}` to PreToolUse -- prevents the action |
| `inject` | Inject a guidance message into the agent's context |
| `notify` | Log a warning without blocking |

## Complementary Tools

Squawk focuses exclusively on stateful multi-event pattern detection. For
stateless single-event guards, use these established tools:

| Need | Tool |
|------|------|
| Destructive command blocking | [hardstop](https://github.com/frmoretto/hardstop) |
| Git safety and security config | [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config) |
| File protection and quality gates | [claudekit](https://github.com/carlrannaberg/claudekit) |
| TDD enforcement | [tdd-guard](https://github.com/nizos/tdd-guard) |
| Prompt injection defense | [lasso-security/claude-hooks](https://github.com/lasso-security/claude-hooks) |

`squawk init` recommends these tools alongside its own configuration.

## Architecture

```
cmd/squawk/          CLI entrypoint (cobra)
internal/
  types/             Shared types: Event, Rule, Condition, Action, Activity
  rules/             Rule engine: YAML parsing, condition evaluation
  monitor/           HTTP hook server, activity tracker (sliding window)
  action/            Action executor (block, inject, notify) and log writer
  config/            Config loading/saving, hooks snippet generation
rules/               Default and community rule YAML files
```

Key design decisions:

- Zero external dependencies beyond cobra and yaml.v3
- In-process sliding window -- no database, no Redis
- Rules are pure YAML -- no scripting language, no Rego
- Fail-open: if squawk is not running, hooks silently succeed (`|| true`)

## Contributing

Contributions are welcome. To get started:

```bash
git clone https://github.com/Jack-Lin-DS-AI/squawk.git
cd squawk
go build ./...
go test -race ./...
```

### Adding rules

1. Create a YAML file in `rules/community/`
2. Follow the condition schema in `internal/types/types.go`
3. Add a test scenario in `cmd/squawk/main.go` (`buildTestScenario`)
4. Run `squawk rules test --scenario your-scenario` to verify

### Code style

- `gofmt` and `goimports` are mandatory
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`
- Table-driven tests with the `-race` flag

## License

MIT
