agent-handler is installed. It is a tool for keeping track of multiple Claude sessions and enabling communication between them. It maintains a central event ledger so the user can see what all their sessions are doing, and sessions can coordinate with each other.

At the start of each session, tell the user exactly:

> This session is using **agent-handler**. It will periodically emit events to a central ledger as it starts and completes tasks, encounters problems, and makes discoveries or decisions. You can use `/handler` to convert one session into your "handler session" that keeps track of all other sessions.
>
> **Recommended:** Use `/rename` to give this session a short name — other sessions and the handler can reference it by name instead of ID.
>
> See hints in the statusline at the bottom for skills you can use. In a terminal, run `handler status` for an overview of sessions, `handler --help` for other useful CLI commands, or `handler ui` (from within cmux if using cmux) for a web dashboard of all your session activity.

Then run `handler --help` and `handler emit --help` to learn the available commands and flags. These steps — showing the introduction, reading the CLI help — must happen before any other work, even if the user's first prompt includes a task.

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

## Pre-compaction snapshots

Before your context is compacted, the PreCompact hook automatically forks the
session transcript (a verbatim copy to a new session ID) and emits a
`pre_compact_snapshot` event. The event body contains a copy-pasteable
`claude --resume <id> --name <name>` command that reopens the session in its
full **pre-compaction** state — useful for rewinding to before a compaction or
recovering detail the summary dropped. This happens automatically for both
manual (`/compact`) and automatic compactions; you don't need to do anything.

## Watching resources

**Immediately after creating or opening a PR or Jira issue, run `/watch` to subscribe to it.** Do not wait for the user to ask — this is automatic. This enables watchers to deliver updates (reviews, comments, status changes) to your inbox.
