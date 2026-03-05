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
| **FILEREG** | File Registry | Track known file paths; detect Write on already-seen files |
| **DIFF+CNT** | Diff Pattern + Counter | Regex on edit content, counted over time |

---

## Implemented Rules

### `test-only-modification` — Modifying Tests Without Checking Source

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS + SRC (3 cross-correlated conditions with source_of) |
| **Severity** | CRITICAL |
| **Action** | block (30s cooldown) |

**Trigger (AND logic, all within 5 minutes):**
1. Edit/Write on test files (`*_test.go|*.test.ts|*.test.js|*.spec.ts|*.spec.js`) — count >= 3
2. Read/Grep/Glob of the **corresponding source files** (via `source_of: 0`) — count < 1 (negated: no related source exploration)
3. Edit/Write on NON-test files — count < 1 (negated: no source code changes)

**Resets when:** Agent reads the corresponding source file (e.g., `calc.go` for `calc_test.go`) OR edits a non-test file.
Reading unrelated files does NOT reset the counter.

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

### `edit-oscillation` — Code Oscillation (A→B→A)

| Field | Value |
|-------|-------|
| **Technique** | CHASH |
| **Severity** | CRITICAL |
| **Action** | block (30s cooldown) |

**Trigger:** Edit/Write on same file reverts content to a previously seen state within 10 minutes. Uses FNV-1a content hashing per file.

---

### `repeated-identical-edit` — Same Edit Applied Repeatedly

| Field | Value |
|-------|-------|
| **Technique** | EHASH |
| **Severity** | HIGH |
| **Action** | block (30s cooldown) |

**Trigger:** The exact same (file_path, old_string, new_string) Edit is applied 3+ times within 5 minutes.

---

### `repeated-failing-command` — Exact Same Command Failing

| Field | Value |
|-------|-------|
| **Technique** | CMDHASH |
| **Severity** | HIGH |
| **Action** | block (60s cooldown) |

**Trigger:** The exact same Bash command fails (PostToolUseFailure) 3+ times within 3 minutes. Upgrade of `excessive-retry-same-command` with command hash matching.

---

### `whole-file-rewrite` — Write on Known Files

| Field | Value |
|-------|-------|
| **Technique** | FILEREG |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:** Write tool used on files already seen in Read/Edit activities, 2+ times within 5 minutes.

---

### `test-assertion-weakening` — Removing Test Assertions

| Field | Value |
|-------|-------|
| **Technique** | DIFF+CNT |
| **Severity** | CRITICAL |
| **Action** | block (30s cooldown) |

**Trigger:** Edit on test files removes assertion patterns (`assert`, `expect`, `require.`, etc.) 2+ times within 5 minutes. Detected when old_string matches pattern but new_string does not.

---

### `error-handling-removal` — Removing Error Handling

| Field | Value |
|-------|-------|
| **Technique** | DIFF+CNT |
| **Severity** | HIGH |
| **Action** | block (30s cooldown) |

**Trigger:** Edit removes error handling patterns (`if err != nil`, `try {`, `catch (`, etc.) 2+ times within 5 minutes.

---

### `large-code-deletion` — Repeated Large Deletions

| Field | Value |
|-------|-------|
| **Technique** | DIFF+CNT |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:** Edit reduces code by >50% (`len(new) < 0.5 * len(old)`) 3+ times within 5 minutes.

---

## Summary

| Rule | Technique | Severity | Action |
|------|-----------|----------|--------|
| test-only-modification | CNT+ABS | CRITICAL | block |
| edit-oscillation | CHASH | CRITICAL | block |
| test-assertion-weakening | DIFF+CNT | CRITICAL | block |
| excessive-retry-same-command | CNT | HIGH | block |
| repeated-identical-edit | EHASH | HIGH | block |
| repeated-failing-command | CMDHASH | HIGH | block |
| error-handling-removal | DIFF+CNT | HIGH | block |
| same-file-excessive-edits | CNT | MEDIUM | inject |
| blind-file-creation | CNT+ABS | MEDIUM | inject |
| whole-file-rewrite | FILEREG | MEDIUM | inject |
| large-code-deletion | DIFF+CNT | MEDIUM | inject |
| write-before-read | CNT+ABS | LOW | inject |
| session-context-warning | CNT | LOW | inject |

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
