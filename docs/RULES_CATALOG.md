# Squawk Rules Catalog

## Positioning

**Squawk = stateful cross-event behavioral pattern detection.**

Single-event stateless guards (block `rm -rf`, protect `.env`, etc.) are already well-served
by existing tools: [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config),
[hardstop](https://github.com/frmoretto/hardstop), [claudekit](https://github.com/carlrannaberg/claudekit),
[tdd-guard](https://github.com/nizos/tdd-guard), etc.

Squawk focuses on what **no existing tool does**: detecting behavioral patterns that emerge
across multiple tool calls over time — loops, oscillation, escalating bad habits, and
cross-event correlations that stateless hooks cannot see.

---

## What Squawk Does NOT Do (Use Existing Tools)

These are single-event checks. `squawk init` will recommend installing existing tools for these.

| Pattern | Existing Tool |
|---------|--------------|
| Block `rm -rf`, `git reset --hard` | [hardstop](https://github.com/frmoretto/hardstop) (428 patterns) |
| Block force push | [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config) |
| Protect `.env`, `.pem`, credential files | [claudekit file-guard](https://github.com/carlrannaberg/claudekit) (195 patterns) |
| Detect hardcoded secrets | [claudekit](https://github.com/carlrannaberg/claudekit), GitGuardian |
| Block `--no-verify`, `--no-gpg-sign` | [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config) |
| SQL injection patterns | [anthropics/claude-plugins-official](https://github.com/anthropics/claude-plugins-official) security-guidance |
| Prompt injection defense | [lasso-security/claude-hooks](https://github.com/lasso-security/claude-hooks), [NOVA](https://github.com/fr0gger/nova-claude-code-protector) |
| TDD enforcement (test-first) | [tdd-guard](https://github.com/nizos/tdd-guard) |
| Auto-format after edit | [claudekit](https://github.com/carlrannaberg/claudekit), plain PostToolUse hook |
| Auto-lint after edit | [claudekit](https://github.com/carlrannaberg/claudekit), [decider/claude-hooks](https://github.com/decider/claude-hooks) |

---

## Detection Techniques (Squawk-Only)

| Code | Technique | Description | Why Stateless Hooks Can't Do This |
|------|-----------|-------------|----------------------------------|
| **CNT** | Counter | Count events matching criteria within a time window | Needs memory of past events |
| **CNT+ABS** | Counter + Absence | Count event A occurring AND event B NOT occurring | Needs cross-event correlation |
| **CHASH** | Content Hash | Hash file content after each edit; detect reversion to previous state | Needs history of all file states |
| **EHASH** | Edit Hash | Hash(old_string + new_string); detect identical edits repeated | Needs history of all edits |
| **CMDHASH** | Command Hash | Hash(bash command); detect identical failing commands | Needs history of all commands |
| **DIFF+CNT** | Diff Pattern + Counter | Regex on edit content, counted over time | Single occurrence may be legitimate; pattern over time is the signal |

---

## Rule 1: `edit-oscillation` — A→B→A Code Oscillation

Agent changes code back and forth, reverting to previously seen file states.
This is the #1 most wasted-time pattern reported across all AI coding agents.

| Field | Value |
|-------|-------|
| **Technique** | CHASH |
| **Why squawk** | Needs full history of file content hashes — impossible for stateless hook |
| **Severity** | CRITICAL |
| **Action** | block |

**Trigger:**
- File content hash (after Edit/Write) matches a previously seen hash for that file
- Happens 2+ times within 10 minutes
- Scope: Block Edit/Write on the oscillating file only

**Message:**
```
STOP — You are going back and forth on {file}.
The file content has reverted to a state you already tried.
This is a sign you are stuck in a loop. Step back and:
1. Re-read the error message or test failure output carefully
2. Read related source/dependency files you haven't looked at yet
3. Consider a fundamentally different approach
```

**Example scenario:**
```
14:00  Edit auth.go → hash: abc123
14:01  Edit auth.go → hash: def456    (try approach A)
14:02  Edit auth.go → hash: abc123    ← reverted! (1st oscillation)
14:03  Edit auth.go → hash: def456    ← reverted again! (2nd oscillation)
14:03  squawk BLOCKS next Edit on auth.go
```

---

## Rule 2: `repeated-identical-edit` — Same Edit Applied Repeatedly

Agent applies the exact same old_string→new_string transformation multiple times.
Distinct from oscillation: the file doesn't revert, but the same "fix" keeps being applied
(possibly to different files, or re-applied after being reverted by a test failure).

| Field | Value |
|-------|-------|
| **Technique** | EHASH |
| **Why squawk** | Needs history of all edit fingerprints across the session |
| **Severity** | HIGH |
| **Action** | block |

**Trigger:**
- hash(old_string + new_string) appears 3+ times within 5 minutes
- Scope: Block Edit

**Message:**
```
You have applied the exact same edit {count} times.
If the first attempt didn't solve the problem, repeating it won't either.
Read the error output and try a different fix.
```

---

## Rule 3: `repeated-failing-command` — Same Failing Command Retried

Agent runs the same Bash command that keeps failing with the same error.
Upgrade from existing CNT-only rule to use command hashing for exact-match detection.

| Field | Value |
|-------|-------|
| **Technique** | CMDHASH |
| **Why squawk** | Needs history of command fingerprints + failure correlation |
| **Severity** | HIGH |
| **Action** | block |

**Trigger:**
- Same command hash appears in PostToolUseFailure 3+ times within 3 minutes
- Scope: Block Bash

**Message:**
```
The command `{command}` has failed {count} times with the same error.
Stop retrying and investigate the root cause:
1. Read the error message — what is it actually telling you?
2. Check if a dependency is missing or a service is down
3. Try a completely different approach
```

---

## Rule 4: `same-file-excessive-edits` — Thrashing on One File

Agent edits the same file many times in a short period, even with different edits.
Indicates the agent is struggling and making incremental guesses instead of understanding the problem.

| Field | Value |
|-------|-------|
| **Technique** | CNT (per-file) |
| **Why squawk** | Needs per-file edit count over a time window |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:**
- Same file path in Edit/Write tool_input 6+ times within 5 minutes
- Scope: Inject on next Edit of that file

**Message:**
```
You have edited {file} {count} times in the last 5 minutes.
This suggests you may be struggling with this file. Consider:
1. Reading related files for context you might be missing
2. Running tests to get concrete feedback on what's actually wrong
3. Stepping back to reconsider your approach entirely
```

---

## Rule 5: `test-only-modification` — Modifying Tests Without Checking Source *(existing)*

Agent repeatedly edits test files without reading or editing the implementation code.
The classic "make the test pass by changing the test" anti-pattern.

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS (3 cross-correlated conditions) |
| **Why squawk** | Needs simultaneous tracking of: test edits, absence of reads, absence of source edits |
| **Severity** | CRITICAL |
| **Action** | block |

**Trigger (AND logic, all within 5 minutes):**
1. Edit/Write on test files (`*_test.go|*.test.ts|*.test.js|*.spec.ts|*.spec.js`) — count >= 3
2. Read/Glob/Grep on ANY file — count < 1 (negated: no exploration happened)
3. Edit/Write on NON-test files — count < 1 (negated: no source code changes)

**Scope:** Block Edit/Write on test files only

**Message:**
```
You have modified test files {count} times without reading the source code.
Please read the implementation file first to understand why the test is failing.
The issue is likely in the source code, not the test.
```

**Resets when:** Agent reads any file OR edits a non-test file.

---

## Rule 6: `test-assertion-weakening` — Weakening Assertions Over Time

Agent progressively weakens test assertions across multiple edits to make tests pass.
A single weakening might be legitimate; a pattern of repeated weakening is gaming the tests.

| Field | Value |
|-------|-------|
| **Technique** | DIFF + CNT |
| **Why squawk** | Single occurrence might be valid refactoring; 2+ within 10m is a pattern |
| **Severity** | HIGH |
| **Action** | block |

**Trigger:**
- Edit on test file where old_string→new_string matches a weakening pattern
- 2+ such edits within 10 minutes

**Weakening patterns (old → new):**

| Language | Strong → Weak |
|----------|--------------|
| Go | `assert.Equal(expected, actual)` → `assert.NotNil(actual)` |
| Go | `assert.Equal(expected, actual)` → `assert.True(...)` |
| Go | `t.Fatalf("expected %v")` → removed |
| JS/TS | `expect(x).toBe(exact)` → `expect(x).toBeTruthy()` |
| JS/TS | `expect(x).toEqual(obj)` → `expect(x).toBeDefined()` |
| Python | `assertEqual(a, b)` → `assertTrue(a)` |
| Any | Assertion line removed entirely |
| Any | Test function/block deleted |

**Scope:** Block Edit on test files

**Message:**
```
STOP — You are weakening test assertions instead of fixing the implementation.
Changing assertEqual to assertTrue, or removing assertions, masks bugs rather than fixing them.
The test was written to verify specific behavior — fix the code to match the expected behavior.
Read the implementation file and fix the actual bug.
```

---

## Rule 7: `error-handling-removal` — Stripping Safety Nets Over Time

Agent progressively removes error handling code across multiple edits.
A single removal might be intentional cleanup; a pattern indicates the agent is
"solving" problems by removing the code that reports them.

| Field | Value |
|-------|-------|
| **Technique** | DIFF + CNT |
| **Why squawk** | Pattern over time distinguishes cleanup from reckless removal |
| **Severity** | HIGH |
| **Action** | block |

**Trigger:**
- Edit where old_string contains error handling and new_string removes it
- 2+ such edits within 10 minutes

**Error handling patterns by language:**

| Language | Pattern |
|----------|---------|
| Go | `if err != nil {` block removed |
| Go | `return fmt.Errorf(...)` → removed |
| JS/TS | `try {` ... `} catch` block removed |
| JS/TS | `.catch(` handler removed |
| Python | `try:` ... `except` block removed |
| Any | `validate(`, `sanitize(`, `check(` calls removed |

**Scope:** Block Edit on files where removal detected

**Message:**
```
STOP — You are removing error handling code.
Error handling exists to prevent crashes and data corruption in production.
If error handling is causing a test failure, fix the root cause — don't remove the safety net.
If you believe the error handling is genuinely unnecessary, explain your reasoning.
```

---

## Rule 8: `blind-file-creation` — Creating Without Exploring *(existing)*

Agent creates multiple new files without reading any existing code first.
Leads to duplicated functionality and convention violations.

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS |
| **Why squawk** | Needs cross-correlation: high writes + zero reads in the same window |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger (AND logic, within 5 minutes):**
1. Write tool calls — count >= 3
2. Read/Glob/Grep calls — count < 1 (negated)

**Message:**
```
You are creating many new files without reading existing code.
Please explore the codebase first to avoid duplicating existing functionality.
Use Glob to find related files and Read to understand existing patterns.
```

---

## Rule 9: `write-before-read` — Coding Without Reconnaissance

Agent starts a session by immediately writing/editing code with zero exploration.
Indicates skipping the "understand before modify" phase entirely.

| Field | Value |
|-------|-------|
| **Technique** | CNT + ABS (session-start window) |
| **Why squawk** | Needs to track session start and ordering of first N tool calls |
| **Severity** | LOW |
| **Action** | inject |

**Trigger (AND logic, within first 2 minutes of session):**
1. Edit/Write calls — count >= 3
2. Read/Glob/Grep calls — count < 1 (negated)

**Message:**
```
You started modifying code without exploring the codebase first.
Before writing code, you should:
1. Read existing files to understand current patterns and conventions
2. Use Glob/Grep to find related implementations
3. Understand the project structure before adding to it
```

---

## Rule 10: `whole-file-rewrite` — Rewriting Instead of Editing

Agent uses Write to overwrite entire existing files when targeted Edit would be safer.
Risks losing code, breaks git blame history, and increases review difficulty.

| Field | Value |
|-------|-------|
| **Technique** | CHASH + CNT |
| **Why squawk** | Needs to know if a file already existed (had a previous content hash) |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:**
- Write tool used on a file that already has a content hash in history (= file existed)
- 2+ such overwrites within 5 minutes

**Message:**
```
You are rewriting entire files instead of making targeted edits.
Use the Edit tool to modify specific sections — this is safer, preserves git blame history,
and reduces the risk of accidentally removing working code.
```

---

## Rule 11: `large-code-deletion` — Suspiciously Large Removals

Agent removes significantly more code than it adds in a single edit, repeatedly.
A single large deletion might be legitimate cleanup; repeated large deletions
suggest the agent is "simplifying" by gutting functionality.

| Field | Value |
|-------|-------|
| **Technique** | DIFF + CNT |
| **Why squawk** | Pattern of repeated large deletions is the signal, not a single one |
| **Severity** | MEDIUM |
| **Action** | inject |

**Trigger:**
- Edit where len(old_string) > len(new_string) × 3 AND len(old_string) > 200 chars
- 3+ such edits within 10 minutes

**Message:**
```
WARNING — You have made {count} edits that remove significantly more code than they add.
Verify that the removed code is truly unnecessary.
Deleted functionality is hard to recover and may silently break dependent code.
If refactoring, prefer smaller incremental changes that can be verified with tests.
```

---

## Rule 12: `session-context-warning` — Session Getting Long

Session has had many tool interactions without a clear break, increasing risk
of context degradation (lost instructions, forgotten constraints).

| Field | Value |
|-------|-------|
| **Technique** | CNT (session-level) |
| **Why squawk** | Needs cumulative session activity count |
| **Severity** | LOW |
| **Action** | notify |

**Trigger:**
- 50+ PostToolUse events in the current session

**Message:**
```
This session has had {count} tool interactions.
Context degradation may cause instruction drift.
Consider running /compact with a task summary, or /clear if switching tasks.
```

---

## Summary

| # | Rule | Technique | Severity | Action | Unique to Squawk? |
|---|------|-----------|----------|--------|-------------------|
| 1 | edit-oscillation | CHASH | CRITICAL | block | Yes — needs file hash history |
| 2 | repeated-identical-edit | EHASH | CRITICAL | block | Yes — needs edit fingerprint history |
| 3 | repeated-failing-command | CMDHASH | HIGH | block | Yes — needs command hash + failure tracking |
| 4 | same-file-excessive-edits | CNT | MEDIUM | inject | Yes — needs per-file count over time |
| 5 | test-only-modification | CNT+ABS | CRITICAL | block | Yes — needs 3-way cross-correlation |
| 6 | test-assertion-weakening | DIFF+CNT | HIGH | block | Yes — single edit is ambiguous; pattern is the signal |
| 7 | error-handling-removal | DIFF+CNT | HIGH | block | Yes — single removal might be cleanup; pattern is reckless |
| 8 | blind-file-creation | CNT+ABS | MEDIUM | inject | Yes — needs write count + absence of reads |
| 9 | write-before-read | CNT+ABS | LOW | inject | Yes — needs session-start activity ordering |
| 10 | whole-file-rewrite | CHASH+CNT | MEDIUM | inject | Yes — needs knowledge of file's prior existence |
| 11 | large-code-deletion | DIFF+CNT | MEDIUM | inject | Yes — pattern of repeated deletion is the signal |
| 12 | session-context-warning | CNT | LOW | notify | Yes — needs cumulative session count |

**Every rule requires state that stateless hooks cannot provide.** This is squawk's moat.

---

## Implementation Phases

### Phase 1: Counter Engine (current — already works)
Rules: 4, 5, 8, 9, 12

Engine changes: None. These work with the existing CNT + CNT+ABS conditions.
New rules to add to default.yaml: 4, 9, 12 (5 and 8 already exist).

### Phase 2: Hash Engine
Rules: 1, 2, 3, 10

Engine changes:
- Activity struct gets `ContentHash`, `EditHash`, `CommandHash` fields
- Monitor computes hashes from tool_input on PostToolUse
- New condition fields: `content_hash_repeated: true`, `edit_hash_repeated: true`, `command_hash_repeated: true`
- Hash history stored per-file (content) or globally (edit/command) in the activity tracker

### Phase 3: Diff Engine
Rules: 6, 7, 11

Engine changes:
- New condition fields: `diff_pattern`, `diff_removed_pattern` (regex matched against old_string)
- New condition field: `diff_size_ratio` (old/new length ratio threshold)
- Engine extracts old_string/new_string from tool_input for Edit events

### Future: LLM Integration (via Claude Code prompt hooks)
- Scope creep detection
- Over-engineering detection
- Sycophancy / unjustified reversion detection

These don't need squawk engine changes — `squawk init` generates prompt hooks
in ~/.claude/settings.json that use Claude Code's built-in Haiku evaluation.

---

## Complementary Tools (recommended by `squawk init`)

Squawk focuses on stateful behavioral patterns. For stateless single-event guards,
`squawk init` recommends and optionally installs:

| Need | Recommended Tool | Stars |
|------|-----------------|-------|
| Destructive command blocking | [hardstop](https://github.com/frmoretto/hardstop) | ~19 |
| Git safety + security config | [trailofbits/claude-code-config](https://github.com/trailofbits/claude-code-config) | ~1,500 |
| File protection + quality gates | [claudekit](https://github.com/carlrannaberg/claudekit) | ~617 |
| TDD enforcement | [tdd-guard](https://github.com/nizos/tdd-guard) | ~1,800 |
| Prompt injection defense | [lasso-security/claude-hooks](https://github.com/lasso-security/claude-hooks) | 123 |
| Permission fatigue reduction | [Dippy](https://github.com/ldayton/Dippy) | ~35 |
