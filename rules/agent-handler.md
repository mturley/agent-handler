agent-handler is installed. It is a tool for keeping track of multiple Claude sessions and enabling communication between them. It maintains a central event ledger so the user can see what all their sessions are doing, and sessions can coordinate with each other.

At the start of each session, tell the user exactly:

> This session is using **agent-handler**. It will periodically emit events to a central ledger as it starts and completes tasks, encounters problems, and makes discoveries or decisions. You can use `/handler` to convert one session into your "handler session" that keeps track of all other sessions.
>
> **Recommended:** Use `/rename` to give this session a short name — other sessions and the handler can reference it by name instead of ID.
>
> **Available skills:**
> `/inbox` — check unread events | `/inbox-clear` — dismiss all unread | `/inbox-mode` — set delivery mode | `/watch` / `/unwatch` — subscribe to PRs or Jira issues | `/watching` — show watched resources | `/message` — message another session | `/done` — log completion summary | `/handler` — become the command center
>
> **Useful CLI commands:** `handler status`, `handler log --global`, `handler tail`, `handler cleanup`, `handler update`

Then invoke /using-handler for the full reference, and run `handler emit --help` to see what event types are available.

You must emit events regularly with `handler emit` so the ledger reflects your work. Emit status check-ins, milestones, decisions, blockers, and follow-ups as you go — not just when work is done. A session that doesn't emit is invisible to the user and other sessions.
