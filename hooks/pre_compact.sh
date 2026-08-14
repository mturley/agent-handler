#!/usr/bin/env bash
# PreCompact hook for agent-handler.
# Forks the session transcript before compaction and emits a pre_compact_snapshot
# event with a copy-pasteable resume command. All logic lives in Go
# (`handler fork-snapshot`); this hook is a thin pipe. It must never block
# compaction, so it always exits 0.
set -uo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

# The PreCompact hook receives a JSON payload on stdin (session_id,
# transcript_path, trigger, custom_instructions). Forward it to handler.
handler fork-snapshot >&2 || echo "agent-handler: fork-snapshot failed (compaction proceeding)" >&2

exit 0
