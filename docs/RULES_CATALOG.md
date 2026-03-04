# Squawk Rules Catalog

## Positioning

**Squawk = stateful cross-event behavioral pattern detection.**

Single-event stateless guards (block `rm -rf`, protect `.env`, etc.) are already well-served
by existing tools: [hardstop](https://github.com/frmoretto/hardstop),
[trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config),
[claudekit](https://github.com/carlrannaberg/claudekit),
[tdd-guard](https://github.com/nizos/tdd-guard), etc.

Squawk focuses on what **no existing tool does**: detecting behavioral patterns that emerge
across multiple tool calls over time — loops, oscillation, and cross-event correlations
that stateless hooks cannot see.

---

## Detection Techniques

| Code | Technique | Description |
|------|-----------|-------------|
| **CNT** | Counter | Count events matching criteria within a time window |
| **CNT+ABS** | Counter + Absence | Count event A occurring AND event B NOT occurring |
| **CHASH** | Content Hash | Hash file content; detect reversion to previous state |
| **EHASH** | Edit Hash | Hash edits; detect identical transformations repeated |
| **CMDHASH** | Command Hash | Hash commands; detect identical failing commands |
| **DIFF+CNT** | Diff Pattern + Counter | Regex on edit content, counted over time |

---

## Implemented Rules

### `test-only-modification` — Modifying Tests Without Checking Source

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS (3 cross-correlated conditions) |
| **Severity** | CRITICAL |
| **Action** | block (30s cooldown) |

**Trigger (AND logic, all within 5 minutes):**
1. Edit/Write on test files (`*_test.go|*.test.ts|*.test.js|*.spec.ts|*.spec.js`) — count >= 3
2. Read/Glob/Grep on ANY file — count < 1 (negated: no exploration happened)
3. Edit/Write on NON-test files — count < 1 (negated: no source code changes)

**Resets when:** Agent reads any file OR edits a non-test file.

---

### `excessive-retry-same-command` — Same Failing Command Retried

| Field | Value |
|-------|-------|
| **Technique** | CNT |
| **Severity** | HIGH |
| **Action** | block (60s cooldown) |

**Trigger:** Bash tool fails (PostToolUseFailure) 3+ times within 3 minutes.

---

### `blind-file-creation` — Creating Without Exploring

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger (AND logic, within 5 minutes):**
1. Write tool calls — count >= 3
2. Read/Glob/Grep calls — count < 1 (negated)

---

### `same-file-excessive-edits` — Thrashing on Files

| Field | Value |
|-------|-------|
| **Technique** | CNT |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:** Edit/Write tool used 8+ times within 5 minutes.

---

### `write-before-read` — Coding Without Reconnaissance

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS |
| **Severity** | LOW |
| **Action** | inject |

**Trigger (AND logic, within 2 minutes):**
1. Edit/Write calls — count >= 3
2. Read/Glob/Grep calls — count < 1 (negated)

---

### `session-context-warning` — Session Getting Long

| Field | Value |
|-------|-------|
| **Technique** | CNT (session-level) |
| **Severity** | LOW |
| **Action** | inject |

**Trigger:** 50+ PostToolUse events within 30 minutes.

---

## Summary

| Rule | Technique | Severity | Action |
|------|-----------|----------|--------|
| test-only-modification | CNT+ABS | CRITICAL | block |
| excessive-retry-same-command | CNT | HIGH | block |
| blind-file-creation | CNT+ABS | MEDIUM | inject |
| same-file-excessive-edits | CNT | MEDIUM | inject |
| write-before-read | CNT+ABS | LOW | inject |
| session-context-warning | CNT | LOW | inject |

---

## Planned Rules

These rules require engine enhancements (hash tracking, diff analysis) not yet implemented.

| Rule | Technique | Description |
|------|-----------|-------------|
| `edit-oscillation` | CHASH | Detect A→B→A code oscillation via file content hashing |
| `repeated-identical-edit` | EHASH | Detect same old→new transformation applied repeatedly |
| `repeated-failing-command` | CMDHASH | Upgrade `excessive-retry` with exact command hash matching |
| `test-assertion-weakening` | DIFF+CNT | Detect progressive weakening of test assertions |
| `error-handling-removal` | DIFF+CNT | Detect progressive removal of error handling code |
| `whole-file-rewrite` | CHASH+CNT | Detect Write on files that already exist (prefer Edit) |
| `large-code-deletion` | DIFF+CNT | Detect repeated edits removing significantly more code than they add |

---

## Complementary Tools

Squawk focuses on stateful behavioral patterns. For stateless single-event guards:

| Need | Recommended Tool |
|------|-----------------|
| Destructive command blocking | [hardstop](https://github.com/frmoretto/hardstop) |
| Git safety + security config | [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config) |
| File protection + quality gates | [claudekit](https://github.com/carlrannaberg/claudekit) |
| TDD enforcement | [tdd-guard](https://github.com/nizos/tdd-guard) |
| Prompt injection defense | [lasso-security/claude-hooks](https://github.com/lasso-security/claude-hooks) |
