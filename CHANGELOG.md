# Changelog

All notable changes to Squawk will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - Unreleased

### Added

- **12 built-in behavioral detection rules** across 3 categories (counter-based, hash-based, diff-based) to detect anti-patterns like test-only edits, excessive retries, edit oscillation, and error handling removal
- **Cross-condition `source_of`** — derive source file paths from test files using naming conventions (e.g., `calc_test.go` → `calc.go`) for precise related-file detection
- **Per-file `group_by`** — count activities per file_path independently (used by `same-file-excessive-edits` to track per-file edit counts)
- **Rule management CLI** — list, enable, disable, remove, and interactively create custom rules
- **Rule testing framework** — `squawk rules test` with scenario simulations (test-modification, excessive-retry, blind-creation)
- **Cooldown mechanism** to prevent repetitive rule interventions for the same pattern
- **Real-time action logging** — JSON-lines event log at `.squawk/squawk.log` for metrics and debugging
- **Metrics aggregation** — `squawk stats` command with per-project and time-range filtering
- **Status monitoring** — `squawk status` shows daemon health, hook installation, active sessions, and enabled rule count
- **Custom rule support** via YAML with condition fields for event type, tool regex, file patterns, count thresholds, time windows, hash modes (content/edit/command/known_file), and diff patterns
- **HTTP hook server** — integrates with Claude Code via PreToolUse and PostToolUse hooks for synchronous blocking and asynchronous pattern tracking
- **Admin API** with token authentication for hot-reloading rules and accessing metrics
- **File hotspots reporting** — identifies top 10 most-edited files in activity metrics
- **Daemon lifecycle management** — `squawk setup` (one-command bootstrap), `squawk stop`, `squawk teardown`
- **GitHub Actions CI** with Go 1.24/1.25 matrix, test coverage, vet, and lint checks

### Fixed

- Block rules no longer waste cooldown on PostToolUse evaluations (cooldown only set when block is enforced on PreToolUse)

### Security

- Fix critical command injection vulnerability in osascript sendNotification
- Harden file permissions to 0600 for PID, log, and daemon files
- Add log rotation at 10MB with size-based trigger
- Implement token-based authentication for admin API endpoints
