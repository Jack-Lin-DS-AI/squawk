# Squawk

[![CI](https://github.com/Jack-Lin-DS-AI/squawk/actions/workflows/ci.yml/badge.svg)](https://github.com/Jack-Lin-DS-AI/squawk/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.24+-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Stateful behavioral pattern detection for AI coding agents.

Squawk monitors [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
tool usage via hooks and detects behavioral anti-patterns that emerge across
multiple actions over time — loops, oscillation, escalating bad habits — that
no single-event hook can catch.

<p align="center">
  <img src="demo.gif" alt="Squawk demo" width="800">
</p>

## Why Squawk?

AI coding agents can fall into repetitive loops: editing tests without reading
source, retrying failing commands, oscillating between code states. These
patterns waste tokens, time, and context window.

Squawk detects these **cross-event behavioral anti-patterns** in real-time and
intervenes — blocking destructive loops or injecting corrective context. Unlike
single-event hooks, squawk tracks state over time to catch patterns that emerge
across multiple tool calls.

## Install

Pick one:

**Homebrew** (macOS/Linux):

```bash
brew install Jack-Lin-DS-AI/tap/squawk
```

**Binary download** (example: macOS ARM — see [Releases](https://github.com/Jack-Lin-DS-AI/squawk/releases) for all platforms):

```bash
curl -Lo squawk.tar.gz https://github.com/Jack-Lin-DS-AI/squawk/releases/latest/download/squawk_darwin_arm64.tar.gz
tar xzf squawk.tar.gz
sudo mv squawk /usr/local/bin/
```

**Go install**:

```bash
go install github.com/Jack-Lin-DS-AI/squawk/cmd/squawk@latest
```

## Quick Start

```bash
cd your-project && squawk setup
```

Restart Claude Code and squawk is active. `squawk setup` is idempotent.

```bash
squawk status          # daemon + hooks + sessions
squawk stop            # stop the daemon
squawk teardown        # stop + remove hooks
squawk stats           # intervention metrics
squawk log --tail 20   # recent action log
```

## Built-in Rules (12)

**Counter-based** — count events within a time window:

| Rule | Trigger | Action |
|------|---------|--------|
| `test-only-modification` | 3+ test edits, zero reads of corresponding source (5 min) | block |
| `blind-file-creation` | 3+ file creates, zero reads (5 min) | inject |
| `same-file-excessive-edits` | 8+ edits to the same file (5 min) | inject |
| `write-before-read` | 3+ writes, zero reads (2 min) | inject |
| `session-context-warning` | 50+ tool calls (30 min) | inject |

**Hash-based** — detect identical/oscillating operations via FNV-1a hashing:

| Rule | Trigger | Action |
|------|---------|--------|
| `edit-oscillation` | File content reverts to previous state (10 min) | block |
| `repeated-identical-edit` | Same (file, old, new) edit 3+ times (5 min) | block |
| `repeated-failing-command` | Same base command fails 3+ times with any args (3 min) | block |
| `whole-file-rewrite` | Write on already-read/edited files 2+ times (5 min) | inject |

**Diff-based** — regex/ratio analysis on edit content:

| Rule | Trigger | Action |
|------|---------|--------|
| `test-assertion-weakening` | Removes assert/expect from test files 2+ times (5 min) | block |
| `error-handling-removal` | Removes `if err != nil`/`try`/`catch` 2+ times (5 min) | block |
| `large-code-deletion` | Edit shrinks code >50% 3+ times (5 min) | inject |

See [docs/RULES_CATALOG.md](docs/RULES_CATALOG.md) for detailed descriptions.

## Managing Rules

```bash
squawk rules list                      # show all rules
squawk rules enable/disable <name>     # toggle + hot-reload
squawk rules remove <name> --force     # permanently remove
squawk rules add                       # interactive creation
squawk rules test --scenario <name>    # test against simulated events
```

## Custom Rules

Create a YAML file in `rules/`:

```yaml
rules:
  - name: my-custom-rule
    enabled: true
    priority: 5
    trigger:
      logic: and   # "and" (default) or "or"
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

**Condition fields:**

| Field | Type | Description |
|-------|------|-------------|
| `event` | string | Hook event: `PreToolUse`, `PostToolUse`, `PostToolUseFailure` |
| `tool` | regex | Tool name: `"Edit\|Write"`, `"Bash"` |
| `file_pattern` | glob | Include files: `"*_test.go\|*.test.ts"` |
| `file_pattern_exclude` | glob | Exclude files |
| `count` | int | Occurrence threshold (default 1) |
| `within` | duration | Time window: `"5m"`, `"30s"` |
| `negate` | bool | Invert condition (true = absence check) |
| `hash_mode` | string | `"content"`, `"edit"`, `"command"`, `"known_file"` |
| `source_of` | int | Index of another condition; derives source file paths from that condition's matched test files (e.g., `calc_test.go` → `calc.go`) |
| `group_by` | string | `"file"`: count per file_path, trigger when any single file meets the count threshold |
| `diff_pattern` | regex | Pattern present in old_string but absent in new_string |
| `diff_shrink_ratio` | float | 0-1: trigger when `len(new) < ratio * len(old)` |

**Action types:** `block`, `inject`, `notify`, `log`. Optional `cooldown` suppresses re-triggering.

## Design

- **Fail-open**: if squawk is down, Claude Code continues normally
- **Hot-reload**: rule changes take effect immediately
- **Subagent-aware**: rules apply across the entire session including subagents

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for setup,
conventions, and how to write custom rules.

```bash
git clone https://github.com/Jack-Lin-DS-AI/squawk.git && cd squawk
make test
```

## Support

If you find Squawk useful, please consider giving it a ⭐ on GitHub — it helps others discover the project!

## License

MIT
