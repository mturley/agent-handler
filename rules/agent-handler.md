agent-handler is installed. It is a tool for keeping track of multiple Claude sessions and enabling communication between them. It maintains a central event ledger so the user can see what all their sessions are doing, and sessions can coordinate with each other.

Immediately after starting a session, invoke /using-handler for the full reference, and run `handler emit --help` to see what event types are available.

You must emit events regularly with `handler emit` so the ledger reflects your work. Emit status check-ins, milestones, decisions, blockers, and follow-ups as you go — not just when work is done. A session that doesn't emit is invisible to the user and other sessions.
