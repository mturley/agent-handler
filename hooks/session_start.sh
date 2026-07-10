#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Outputs factual context so Claude knows handler is active.
# Registration is handled by the statusline hook.

echo "agent-handler is active for this session."
echo "The /using-handler skill has not been loaded yet."
