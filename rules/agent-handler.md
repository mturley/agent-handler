agent-handler is installed. It is a tool for keeping track of multiple Claude sessions and enabling communication between them. It maintains a central event ledger so the user can see what all their sessions are doing, and sessions can coordinate with each other.

The user can run `handler status` for an overview, `handler ui` for a web
dashboard, and `/rename` to give this session a short name that other sessions
can address. Mention these only if the user asks — do not introduce agent-handler
unprompted.

Run `handler emit --help` (and `handler --help` for the rest of the CLI) if you
need flags beyond what is documented below.

## Emitting events

You must emit events regularly with `handler emit` so the ledger reflects your work. A session that doesn't emit is invisible to the user and other sessions. Your first emit should happen as soon as you begin working on a task.

```
handler emit --type <type> --title "..." [--body "..."] [--to <target>] [--tags "a,b"]
```

Do not use `--broadcast` unless the user specifically asks to broadcast or message all sessions.

| Trigger | Type | Example title |
|---------|------|---------------|
| Starting work on a task | `status` | "Beginning auth middleware refactor" |
| Finish a commit or meaningful code change | `status` | "Implemented retry logic for API client" |
| Push to remote | `status` | "Pushed feature branch auth-refactor" |
| Find the root cause of a bug | `status` | "Root cause: token validated against wrong key" |
| Choose between approaches | `status` | "Chose RS256 over HS256 for token signing" |
| Create or open a PR | `status` | "Opened PR #42 for auth refactor" |
| Create a Jira issue | `status` | "Created RHOAIENG-12345 for token bug" |
| Periodic check-in on current work | `status` | "Still debugging token refresh — narrowed to middleware" |
| Waiting on something external | `blocked` | "Waiting for review on PR #42" |
| Blocker resolved, resuming work | `unblocked` | "PR #42 approved, continuing" |
| Want to tell another session something | `message` | (use `--to <target>`) |

Emit `blocked` whenever you are waiting on something external. Emit `unblocked` when the blocker is resolved. These feed into the handler's blocked session count and triage report.

### Status reminders

If you go too long without emitting a `status` event, the UserPromptSubmit hook
injects a reminder into your context:

> Reminder: it has been 10 turns since your last emitted status. Use `handler emit --type status` to update the ledger with what you have been doing since the last status and what you are doing now. Be concise.

When you see it, emit a status covering what you did since your last one and what
you are doing now — concisely — then carry on with the user's request. Emitting a
status resets the reminder; so does the reminder itself firing, so it will not
nag you on consecutive turns.

Defaults are 10 turns or 3 hours, whichever comes first, measured from your last
status (or from session registration if you have never emitted one). Automated
`/inbox --auto` polls do not count as turns, and sessions with the `handler` role
are exempt. The user can tune or disable it in `~/.agent-handler/config.yaml`:

```yaml
reminders:
  status:
    enabled: true   # false disables the reminder entirely
    turns: 10       # 0 disables the turn-count check
    hours: 3        # 0 disables the elapsed-time check
```

## Watching resources

**Immediately after creating or opening a PR or Jira issue, run `/watch` to subscribe to it.** Do not wait for the user to ask — this is automatic. This enables watchers to deliver updates (reviews, comments, status changes) to your inbox.

## Scheduled cron jobs

Claude Code cron jobs (`CronCreate` / `CronDelete`) created in this session are
tracked in the handler DB automatically — a `PostToolUse` hook records them as
they are created or deleted, and the `Stop` hook reconciles the table each turn
against Claude's authoritative `session_crons` snapshot.

Run `handler crons` to see this session's tracked jobs, or `handler crons --all`
for every session. Both support `--json`.

Reconciliation matters because Claude removes jobs without any tool call: a
one-shot job auto-deletes the moment it fires, and a recurring job auto-expires
after 7 days. The snapshot is what catches those; the create/delete hooks alone
would leave stale rows behind.

Note that these jobs are **in-memory and session-scoped** — they do not survive
the session that created them. Do not use them for anything that must outlive
the session; the handler DB tracks them, but cannot resurrect them.
