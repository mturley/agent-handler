---
name: handler_register
description: Manually register this session with agent-handler
---

# /handler_register — Register Session

This is usually done automatically by the SessionStart or UserPromptSubmit hook. Use this skill only if automatic registration failed.

The hooks handle session discovery, registration, and `.worktree-resources` subscription automatically. If you need to check whether this session is registered, run:

```bash
handler whoami
```

If that returns an error, the session is not registered. Send another prompt and the UserPromptSubmit hook will attempt registration.
