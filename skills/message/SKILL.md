---
name: message
description: "agent-handler: Send a message to another session via handler emit. Use when the user says 'message session-xyz' or 'tell session-xyz about X'."
---

# /message — Send a Message to Another Session

Send a cross-session message using `handler emit`. This is NOT the same as Claude's built-in agent messaging (which only works for subagents). This uses the handler ledger so any session can message any other session by name, branch, or UUID.

## If invoked with no arguments

If the user runs `/message` with no arguments, explain how cross-session messaging works and show active sessions they can message. Do NOT prompt for input — just explain and let them invoke `/message <target> <message>` when ready.

Print something like:

> **Cross-session messaging** lets you talk to other Claude sessions through the handler ledger.
>
> **Usage:** `/message <session-name> <what to say>`
> **Broadcast:** `/message all <what to say>`
>
> Messages are delivered to the recipient's `/inbox`. They'll see them on their next inbox check (automatically in `on-submit` or `auto` mode, or manually via `/inbox`). The recipient can reply with their own `/message` back to this session.

Then run `handler status` and list the active sessions by name so the user can see who's available.

After printing the explanation and session list, stop — do not proceed to the send steps below.

## Step 1: Get your session name

```bash
handler session-name
```

Include this in the message body so the recipient can reply.

## Step 2: Compose and send

Use the target name from the user's args directly — `handler emit` validates `--to` and errors if the session doesn't exist.

```bash
handler emit --type message --to "<target>" --title "<subject>" --body "<message body>

—from session: <your-session-name>"
```

## Step 3: If emit fails with "no session or branch found"

The target name didn't match. Run `handler status` to find similar session names and suggest the closest match to the user.

## Rules

- Always append `\n\n—from session: <your-session-name>` to the body so the recipient can reply
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
