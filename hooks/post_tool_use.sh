#!/bin/bash
# PostToolUse hook (matcher: CronCreate|CronDelete): records/removes the
# session's Claude Code cron jobs in the handler DB. The Stop hook reconciles
# against session_crons, which is what catches jobs that auto-delete on firing.
handler cron-hook < /dev/stdin
