#!/bin/bash
# StopFailure hook: records turns that ended on a rate limit, so the Stop hook
# does not cancel the wake job that failure just made necessary.
handler stop-failure-hook < /dev/stdin 2>/dev/null || true
exit 0
