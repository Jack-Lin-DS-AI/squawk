#!/usr/bin/env bash
# squawk hook script for Claude Code
#
# Reads hook event JSON from stdin (provided by Claude Code), forwards it to
# the squawk server, and translates the response into the format Claude Code
# expects.
#
# Usage in ~/.claude/settings.json:
#   "command": "/path/to/squawk/scripts/hook.sh PreToolUse"
#   "command": "/path/to/squawk/scripts/hook.sh PostToolUse"
#
# Environment variables:
#   SQUAWK_PORT  — squawk server port (default: 3131)
#   SQUAWK_HOST  — squawk server host (default: localhost)
#
# Exit codes:
#   0  — allow (response JSON on stdout if any)
#   2  — block (reason on stderr, PreToolUse only)
#   *  — non-blocking error, Claude Code continues

set -euo pipefail

HOOK_EVENT="${1:-}"
SQUAWK_HOST="${SQUAWK_HOST:-localhost}"
SQUAWK_PORT="${SQUAWK_PORT:-3131}"
BASE_URL="http://${SQUAWK_HOST}:${SQUAWK_PORT}"

# Read the full stdin JSON from Claude Code.
INPUT=$(cat)

# Determine the squawk endpoint from the hook event name.
case "$HOOK_EVENT" in
  PreToolUse)
    ENDPOINT="/hooks/pre-tool-use"
    ;;
  PostToolUse|PostToolUseFailure)
    ENDPOINT="/hooks/post-tool-use"
    ;;
  *)
    ENDPOINT="/hooks/event"
    ;;
esac

# POST to squawk. --max-time ensures we don't hang if squawk is slow.
# --fail-with-body ensures we get the response body even on HTTP errors.
# If squawk is unreachable, fail-open (exit 0) so Claude Code continues.
RESPONSE=$(curl -s --max-time 5 -X POST \
  "${BASE_URL}${ENDPOINT}" \
  -H 'Content-Type: application/json' \
  -d "$INPUT" 2>/dev/null) || {
    # squawk is unreachable — fail-open.
    exit 0
  }

# If squawk returned an empty or whitespace-only response, allow.
if [[ -z "${RESPONSE// /}" ]]; then
  exit 0
fi

# Parse squawk's response. squawk returns:
#   { "decision": "block", "reason": "...", "additionalContext": "..." }
#
# Claude Code expects different formats depending on the hook event:
#
# PreToolUse: hookSpecificOutput with permissionDecision
#   { "hookSpecificOutput": { "hookEventName": "PreToolUse",
#     "permissionDecision": "deny", "permissionDecisionReason": "..." } }
#
# PostToolUse: top-level decision (matches squawk's format already)
#   { "decision": "block", "reason": "..." }
#   OR for injected context:
#   { "hookSpecificOutput": { "hookEventName": "PostToolUse",
#     "additionalContext": "..." } }

# Extract fields from squawk's response.
DECISION=$(echo "$RESPONSE" | jq -r '.decision // empty' 2>/dev/null) || DECISION=""
REASON=$(echo "$RESPONSE" | jq -r '.reason // empty' 2>/dev/null) || REASON=""
ADDITIONAL_CONTEXT=$(echo "$RESPONSE" | jq -r '.additionalContext // empty' 2>/dev/null) || ADDITIONAL_CONTEXT=""

case "$HOOK_EVENT" in
  PreToolUse)
    if [[ "$DECISION" == "block" ]]; then
      # Translate squawk's block into Claude Code's PreToolUse deny format.
      jq -n --arg reason "${REASON:-Blocked by squawk}" '{
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: "deny",
          permissionDecisionReason: $reason
        }
      }'
      exit 0
    fi
    # No block — allow. Pass through any additional context.
    if [[ -n "$ADDITIONAL_CONTEXT" ]]; then
      jq -n --arg ctx "$ADDITIONAL_CONTEXT" '{
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          additionalContext: $ctx
        }
      }'
    fi
    exit 0
    ;;

  PostToolUse|PostToolUseFailure)
    if [[ "$DECISION" == "block" ]]; then
      # PostToolUse uses top-level decision — squawk's format matches.
      jq -n --arg reason "${REASON:-Flagged by squawk}" '{
        decision: "block",
        reason: $reason
      }'
      exit 0
    fi
    # Pass through injected context if present.
    if [[ -n "$ADDITIONAL_CONTEXT" ]]; then
      jq -n --arg ctx "$ADDITIONAL_CONTEXT" '{
        hookSpecificOutput: {
          hookEventName: "PostToolUse",
          additionalContext: $ctx
        }
      }'
    fi
    exit 0
    ;;

  *)
    # Generic events — no decision control, just forward for tracking.
    exit 0
    ;;
esac
