agent-handler is installed. It is a tool for keeping track of multiple Claude sessions and enabling communication between them. It maintains a central event ledger so the user can see what all their sessions are doing, and sessions can coordinate with each other.

At the start of each session, tell the user exactly:

> This session is using **agent-handler**. It will periodically emit events to a central ledger as it starts and completes tasks, encounters problems, and makes discoveries or decisions. You can use `/handler` to convert one session into your "handler session" that keeps track of all other sessions.
>
> **Recommended:** Use `/rename` to give this session a short name — other sessions and the handler can reference it by name instead of ID.
>
> **Available skills:**
> - `/inbox` — check unread events
> - `/inbox-clear` — dismiss all unread
> - `/inbox-mode` — set delivery mode
> - `/watch` / `/unwatch` — subscribe to PRs or Jira issues
> - `/watching` — show watched resources
> - `/message` — message another session
> - `/done` — log completion summary
> - `/handler` — become the command center
>
> **Useful CLI commands:** `handler status`, `handler log --global`, `handler tail`, `handler cleanup`, `handler update`, `handler switch <name>`, `handler switch -a`
>
> If handler was set up within cmux, keyboard shortcuts are available:
> - **cmd+shift+a** — switch to a session awaiting approval
> - **cmd+shift+s** — interactive session switcher

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
| Finish a commit or meaningful code change | `milestone` | "Implemented retry logic for API client" |
| Push to remote | `milestone` | "Pushed feature branch auth-refactor" |
| Find the root cause of a bug | `milestone` | "Root cause: token validated against wrong key" |
| Choose between approaches | `decision` | "Chose RS256 over HS256 for token signing" |
| Waiting on something external (PR review, CI, blocking issue, Slack reply) | `blocked` | "Waiting for review on PR #42" |
| Blocker resolved, resuming work | `unblocked` | "PR #42 approved, continuing" |
| Identify work for another session or later | `handoff` / `followup` | "Tech debt: error messages need i18n" |
| Want to tell another session something | `message` | (use `--to <target>`) |
| Periodic check-in on current work | `status` | "Still debugging token refresh — narrowed to middleware" |

Emit `blocked` whenever you are waiting on something external. Emit `unblocked` when the blocker is resolved. These feed into the handler's blocked session count and triage report.

## Watching resources

When you create or start working on a PR or Jira issue, immediately run `/watch` to subscribe to it. This enables watchers to deliver updates (reviews, comments, status changes) to your inbox.
