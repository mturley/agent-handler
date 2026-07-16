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
> - `/awaiting` — switch to a session awaiting approval (cmux)
> - `/switch` — switch to another session by name (cmux)
>
> **Useful CLI commands:** `handler status`, `handler log --global`, `handler tail`, `handler cleanup`, `handler update`, `handler cmux-switch <name>`, `handler cmux-switch -a`

Then invoke /using-handler for the full reference on emitting events and using handler. These steps — showing the introduction, invoking the skill — must happen before any other work, even if the user's first prompt includes a task.

You must emit events regularly with `handler emit` so the ledger reflects your work. Emit status check-ins, milestones, decisions, blockers, and follow-ups as you go — not just when work is done. A session that doesn't emit is invisible to the user and other sessions. Your first emit should happen as soon as you begin working on a task.

Emit `blocked` whenever you are waiting on something external — a PR review, CI results, a blocking Jira issue, a Slack reply, or a dependency from another session. Emit `unblocked` when the blocker is resolved. These feed into the handler's blocked session count and triage report, so other sessions and the handler know who is stuck and why.
