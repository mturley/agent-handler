---
name: message
description: Send a message to another session via handler emit. Use when the user says "message session-xyz" or "tell session-xyz about X".
---

# /message — Send a Message to Another Session

Send a cross-session message using `handler emit`. This is NOT the same as Claude's built-in agent messaging (which only works for subagents). This uses the handler ledger so any session can message any other session by name, branch, or UUID.

## Step 1: Check emit capabilities

Run `handler emit --help` to see the current flags and supported options.

## Step 2: Identify your session name

Your session name appears in your statusline output and was set when the session registered. If you don't know it, run:

```bash
handler status --json 2>/dev/null | python3 -c "import sys,json; [print(s['session_name']) for s in json.load(sys.stdin) if s.get('session_id')=='YOUR_SESSION_ID']"
```

You'll include this in the message body so the recipient knows who sent it and can reply.

## Step 3: Identify the target

The user will specify a target — a session name, branch name, or session UUID. If unclear, run `handler status` to show active sessions and let the user pick.

## Step 4: Compose and send

```bash
handler emit --type message --to "<target>" --title "<subject>" --body "<message body>

—from session: <your-session-name>"
```

**Rules:**
- Always append `\n\n—from session: <your-session-name>` to the body so the recipient can reply with `--to <your-session-name>`
- Use `--type message` for direct messages
- The `--title` should be a brief subject line
- The `--body` should contain the full message content
- Use `--broadcast` instead of `--to` to message all sessions
- The args passed to `/message` contain the target and the message intent — parse them to fill in `--to` and compose the title/body

## Examples

User says: `/message vllm-toggle-impl here are the test results: all 3 E2E tests passed`

```bash
handler emit --type message \
  --to "vllm-toggle-impl" \
  --title "Test results" \
  --body "All 3 E2E tests passed.

—from session: statusline-model-git-info"
```

User says: `/message all I'm done with the refactor, all tests pass`

```bash
handler emit --type message \
  --broadcast \
  --title "Refactor complete" \
  --body "Done with the refactor, all tests pass.

—from session: statusline-model-git-info"
```
