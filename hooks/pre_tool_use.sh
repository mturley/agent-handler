#!/bin/bash
# PreToolUse hook (matcher: CronCreate): auto-approves agent-handler's own
# rate-limit wake job and nothing else. Never denies — an unrecognised call
# produces no decision and follows normal permission handling.
handler cron-guard < /dev/stdin 2>/dev/null || true
exit 0
