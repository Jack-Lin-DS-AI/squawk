# Squawk

Stateful behavioral pattern detection for AI coding agents.

Squawk monitors [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
tool usage via hooks and detects behavioral anti-patterns that emerge across
multiple actions over time — loops, oscillation, escalating bad habits — that
no single-event hook can catch. It keeps a sliding window of recent activity
and evaluates rules against the full history.

## Quick Start

```bash
# Install
go install github.com/Jack-Lin-DS-AI/squawk/cmd/squawk@latest

# One-command setup: config + hooks + background daemon
cd your-project
squawk setup
```

Restart Claude Code and squawk is active. `squawk setup` is idempotent.

```bash
squawk status                  # daemon + hooks + sessions
squawk stop                    # stop the daemon
squawk teardown                # stop + remove hooks
squawk stats                   # intervention metrics
squawk log --tail 20           # recent action log
```

## Built-in Rules

| Rule | Trigger | Action |
|------|---------|--------|
| `test-only-modification` | 3+ test edits, zero source reads (5 min) | block (30s cooldown) |
| `excessive-retry-same-command` | Same command fails 3+ times (3 min) | block (60s cooldown) |
| `blind-file-creation` | 3+ file creates, zero reads (5 min) | inject |
| `same-file-excessive-edits` | 8+ edits (5 min) | inject |
| `write-before-read` | 3+ writes, zero reads (2 min) | inject |
| `session-context-warning` | 50+ tool calls (30 min) | inject |

## Managing Rules

```bash
squawk rules list                          # show all rules
squawk rules enable/disable <name>         # toggle + hot-reload
squawk rules remove <name> --force         # permanently remove
squawk rules add                           # interactive creation
squawk rules test --scenario <name>        # test against simulated events
```

## Custom Rules

Create a YAML file in `rules/`:

```yaml
rules:
  - name: my-custom-rule
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
        You have modified config files {count} times.
        Verify these changes are intentional.
```

Condition fields: `event`, `tool` (regex), `file_pattern` / `file_pattern_exclude` (glob), `count`, `within`, `negate`.
Action types: `block`, `inject`, `notify`, `log`.

## Design

- **Fail-open**: if squawk is down, Claude Code continues normally
- **Hot-reload**: rule changes take effect immediately
- **Subagent-aware**: rules apply across the entire session including subagents

## Contributing

```bash
git clone https://github.com/Jack-Lin-DS-AI/squawk.git && cd squawk
go build ./... && go test -race ./...
```

## License

MIT
