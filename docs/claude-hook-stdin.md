# Claude Code Hook Stdin Data

Claude Code passes JSON on stdin to all hook scripts. The available fields vary by hook type — lifecycle hooks (SessionStart, UserPromptSubmit) receive a minimal set, while the statusline hook receives a rich session snapshot.

All data captured from Claude Code version 2.1.198–2.1.201 (July 2026), except where a
section notes a later version. Field availability may change across versions.

## Common Fields (All Hooks)

Every hook receives at least these fields:

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Claude session UUID — the primary identifier |
| `transcript_path` | string | Absolute path to the session's JSONL transcript file |
| `cwd` | string | Current working directory |
| `hook_event_name` | string | The event type (e.g. `"Stop"`, `"UserPromptSubmit"`) |

Note: `prompt_id` is present on most hooks but NOT on `SessionStart`.

## SessionStart

Received when a new Claude session starts (or resumes).

```json
{
  "session_id": "210b7985-8a52-4b53-9c55-335e2f15af96",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley--config-cmux/210b7985-8a52-4b53-9c55-335e2f15af96.jsonl",
  "cwd": "/Users/mturley/.config/cmux",
  "hook_event_name": "SessionStart",
  "source": "startup",
  "model": "claude-opus-4-8[1m]"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `source` | string | How the session started (e.g. `"startup"`) |
| `model` | string | Model identifier as a plain string (not an object — contrast with the statusline's `model.id`) |

Note: SessionStart does NOT include `prompt_id`.

## UserPromptSubmit

Received on each user prompt submission.

```json
{
  "session_id": "ec2d8520-c7e5-4dc0-a206-6b1d5593d471",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-git-agent-ledger/ec2d8520-c7e5-4dc0-a206-6b1d5593d471.jsonl",
  "cwd": "/Users/mturley/git/agent-ledger",
  "prompt_id": "e0658ba4-3950-4d99-bac2-14d986fec548",
  "permission_mode": "acceptEdits",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "Here's another message",
  "session_title": "agent-handler-implementation"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `prompt_id` | string | UUID of the current prompt turn |
| `permission_mode` | string | Current permission mode (`"default"`, `"acceptEdits"`, `"plan"`, etc.) |
| `prompt` | string | The user's prompt text |
| `session_title` | string | Display name of the session (set via `/name` or auto-generated) |

## PostToolUse

Received after a tool call completes. The `matcher` field filters by **tool name**
(exact string or regex) and works for any tool, including built-ins — `"CronCreate|CronDelete"`
is a valid matcher. (Verified 2026-08-31 on 2.1.248.)

```json
{
  "session_id": "3cb5b8fb-a547-4831-b660-ef782dda3660",
  "transcript_path": "/Users/mturley/.claude-personal/projects/-Users-mturley-git-agent-handler/3cb5b8fb-a547-4831-b660-ef782dda3660.jsonl",
  "cwd": "/Users/mturley/git/agent-handler",
  "prompt_id": "ff9550fe-ed2a-41e6-95d8-599d9981a0a7",
  "permission_mode": "auto",
  "effort": {
    "level": "medium"
  },
  "hook_event_name": "PostToolUse",
  "tool_name": "CronCreate",
  "tool_input": {
    "cron": "23 11 31 8 *",
    "prompt": "Scheduled test fire for tool-use hooks...",
    "recurring": false
  },
  "tool_response": {
    "id": "f158ff6e",
    "humanSchedule": "23 11 31 8 *",
    "recurring": false,
    "durable": false
  },
  "tool_use_id": "toolu_01CMEijY1ncNKfqkxnaTPRXr",
  "duration_ms": 1
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `tool_name` | string | Name of the tool that ran (e.g. `"CronCreate"`, `"Edit"`) |
| `tool_input` | object | The arguments the tool was called with — shape varies per tool |
| `tool_response` | object | The tool's result — shape varies per tool |
| `tool_use_id` | string | `toolu_`-prefixed id, correlates with the transcript JSONL entry |
| `duration_ms` | number | How long the tool call took |

### Cron tool payloads

For `CronCreate`, `tool_input` carries `cron`, `prompt`, and `recurring`; `tool_response`
carries the assigned job `id`, plus `humanSchedule`, `recurring`, and `durable`. For
`CronDelete`, both `tool_input` and `tool_response` are just `{"id": "<job id>"}`.

`durable` is echoed back as `false` — the `CronCreate` tool accepts a `durable` parameter
but documents it as having no effect, and this confirms it at runtime.

**These events are deltas, not state.** See "Tracking a session's cron jobs" under Key
Observations — `session_crons` on Stop is the better source.

## Stop

Received at the end of each assistant turn (once per turn, not per tool call). A Stop hook that exits 2 forces Claude to keep going.

```json
{
  "session_id": "a91e7804-d26d-437f-aa68-2c36adaa7fc3",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-git-agent-ledger/a91e7804-d26d-437f-aa68-2c36adaa7fc3.jsonl",
  "cwd": "/Users/mturley/git/agent-ledger",
  "prompt_id": "49429436-b0a7-442c-933a-3da1b98f89d4",
  "permission_mode": "default",
  "effort": {
    "level": "medium"
  },
  "hook_event_name": "Stop",
  "stop_hook_active": false,
  "last_assistant_message": "Here is the assistant's final message text...",
  "background_tasks": [],
  "session_crons": [
    {
      "id": "a2f2427f",
      "schedule": "*/1 * * * *",
      "recurring": true,
      "prompt": "/inbox --auto"
    }
  ]
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `prompt_id` | string | UUID of the current prompt turn |
| `permission_mode` | string | Current permission mode |
| `effort.level` | string | Reasoning effort (`"low"`, `"medium"`, `"high"`, `"xhigh"`, `"max"`) |
| `stop_hook_active` | boolean | Whether a stop hook is currently preventing the agent from stopping |
| `last_assistant_message` | string | The assistant's final message text for this turn (may be absent if empty) |
| `background_tasks` | array | Currently running background tasks (empty array if none) |
| `session_crons` | array | Scheduled cron jobs in this session. Each entry has `id`, `schedule` (cron expression), `recurring` (boolean), and `prompt` (string) |

Note: Stop does NOT include cost or token data. The statusline hook is the only source of cost information.

## SubagentStart

Received when a subagent is spawned.

```json
{
  "session_id": "a12e3ac4-87ca-4818-8b4e-cb4e41138666",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-git--worktrees-odh-dashboard-breakdown-fast-vllm/a12e3ac4-87ca-4818-8b4e-cb4e41138666.jsonl",
  "cwd": "/Users/mturley/git/.worktrees/odh-dashboard/breakdown-fast-vllm",
  "prompt_id": "3906338d-02fc-46ee-be39-44b68764d33f",
  "agent_id": "ab2e512a320501561",
  "agent_type": "Explore",
  "hook_event_name": "SubagentStart"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `prompt_id` | string | UUID of the parent prompt turn |
| `agent_id` | string | Unique identifier for the subagent |
| `agent_type` | string | Subagent type (e.g. `"Explore"`, `""` for default) |

## SubagentStop

Received when a subagent finishes. Has the same fields as Stop, plus subagent-specific fields.

```json
{
  "session_id": "a12e3ac4-87ca-4818-8b4e-cb4e41138666",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-git--worktrees-odh-dashboard-breakdown-fast-vllm/a12e3ac4-87ca-4818-8b4e-cb4e41138666.jsonl",
  "cwd": "/Users/mturley/git/.worktrees/odh-dashboard/breakdown-fast-vllm",
  "prompt_id": "3906338d-02fc-46ee-be39-44b68764d33f",
  "permission_mode": "acceptEdits",
  "agent_id": "ab2e512a320501561",
  "agent_type": "Explore",
  "effort": {
    "level": "medium"
  },
  "hook_event_name": "SubagentStop",
  "stop_hook_active": false,
  "agent_transcript_path": "/Users/mturley/.claude/projects/.../subagents/agent-ab2e512a320501561.jsonl",
  "last_assistant_message": "The subagent's final message...",
  "background_tasks": [],
  "session_crons": []
}
```

### Additional fields (beyond Stop)

| Field | Type | Description |
|-------|------|-------------|
| `agent_id` | string | Unique identifier for the subagent |
| `agent_type` | string | Subagent type (e.g. `"Explore"`, `""` for default) |
| `agent_transcript_path` | string | Absolute path to the subagent's JSONL transcript |

Note: SubagentStop does NOT include cost or token data.

## Statusline

Received on each statusline refresh (configurable interval, default 10s). Contains the richest data set — the only hook with cost and token information.

```json
{
  "session_id": "530ff9ff-2966-4c28-b2e2-2cca0a019a18",
  "transcript_path": "...",
  "cwd": "...",
  "prompt_id": "ddf555dd-4387-4695-b7ed-27f26029b497",
  "effort": {
    "level": "medium"
  },
  "session_name": "vllm-1-preflight",
  "model": {
    "id": "claude-opus-4-6[1m]",
    "display_name": "Opus 4.6 (1M context)"
  },
  "workspace": {
    "current_dir": "/Users/mturley/git/.worktrees/odh-dashboard/vllm-configs-admin-page",
    "project_dir": "/Users/mturley/git/.worktrees/odh-dashboard/vllm-configs-admin-page",
    "added_dirs": [
      "/Users/mturley/Documents/md-redhat/AI Tool Use"
    ],
    "git_worktree": "vllm-configs-admin-page",
    "repo": {
      "host": "github.com",
      "owner": "mturley",
      "name": "odh-dashboard"
    }
  },
  "version": "2.1.201",
  "output_style": {
    "name": "default"
  },
  "cost": {
    "total_cost_usd": 39.07,
    "total_duration_ms": 8812219,
    "total_api_duration_ms": 1477190,
    "total_lines_added": 73,
    "total_lines_removed": 92
  },
  "context_window": {
    "total_input_tokens": 284564,
    "total_output_tokens": 7,
    "context_window_size": 1000000,
    "current_usage": {
      "input_tokens": 1,
      "output_tokens": 7,
      "cache_creation_input_tokens": 109,
      "cache_read_input_tokens": 284454
    },
    "used_percentage": 28,
    "remaining_percentage": 72
  },
  "exceeds_200k_tokens": true,
  "fast_mode": false,
  "thinking": {
    "enabled": false
  },
  "rate_limits": {
    "five_hour": {
      "used_percentage": 21,
      "resets_at": 1787790000
    },
    "seven_day": {
      "used_percentage": 12,
      "resets_at": 1788296400
    }
  },
  "pr": {
    "number": 8431,
    "url": "https://github.com/opendatahub-io/odh-dashboard/pull/8431",
    "review_state": "pending"
  }
}
```

### Additional fields (beyond common)

| Field | Type | Description |
|-------|------|-------------|
| `session_name` | string | Display name (note: `session_name` here vs `session_title` in UserPromptSubmit) |
| `effort.level` | string | Reasoning effort (`"low"`, `"medium"`, `"high"`, `"xhigh"`, `"max"`) |
| `model.id` | string | Model identifier (e.g. `"claude-opus-4-6[1m]"`) |
| `model.display_name` | string | Human-readable model name |
| `workspace.current_dir` | string | Same as `cwd` |
| `workspace.project_dir` | string | Project root directory |
| `workspace.added_dirs` | string[] | Additional directories added to the session |
| `workspace.git_worktree` | string | Git worktree name (if in a worktree) |
| `workspace.repo.host` | string | Git remote host |
| `workspace.repo.owner` | string | Git remote owner |
| `workspace.repo.name` | string | Git remote repo name |
| `version` | string | Claude Code version |
| `output_style.name` | string | Output style |
| `cost.total_cost_usd` | number | Total API cost for the session (in-memory, resets on laptop restart) |
| `cost.total_duration_ms` | number | Total wall-clock time |
| `cost.total_api_duration_ms` | number | Total time spent in API calls |
| `cost.total_lines_added` | number | Total lines added by edits |
| `cost.total_lines_removed` | number | Total lines removed by edits |
| `context_window.total_input_tokens` | number | Total input tokens used |
| `context_window.total_output_tokens` | number | Total output tokens used |
| `context_window.context_window_size` | number | Model context window size |
| `context_window.current_usage` | object | Current turn token usage (input, output, cache_creation, cache_read) |
| `context_window.used_percentage` | number | Percentage of context window used |
| `context_window.remaining_percentage` | number | Percentage of context window remaining |
| `exceeds_200k_tokens` | boolean | Whether the session has exceeded 200k total tokens |
| `fast_mode` | boolean | Whether fast mode is enabled |
| `thinking.enabled` | boolean | Whether extended thinking is enabled |
| `rate_limits` | object | **First-party Anthropic API sessions only** — absent on Vertex/Bedrock sessions. See below. |
| `rate_limits.five_hour.used_percentage` | number | Percentage of the rolling 5-hour usage limit consumed |
| `rate_limits.five_hour.resets_at` | number | Unix epoch seconds when the 5-hour window resets |
| `rate_limits.seven_day.used_percentage` | number | Percentage of the rolling 7-day usage limit consumed |
| `rate_limits.seven_day.resets_at` | number | Unix epoch seconds when the 7-day window resets |
| `pr.number` | number | Associated PR number (if session is linked to a PR) |
| `pr.url` | string | PR URL |
| `pr.review_state` | string | PR review state (`"pending"`, etc.) |

## SessionEnd

Received when a Claude session closes.

```json
{
  "session_id": "e5cf24d2-009d-4de7-a128-ee1eb0fbda27",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-git-work-scripts/e5cf24d2-009d-4de7-a128-ee1eb0fbda27.jsonl",
  "cwd": "/Users/mturley/git/work-scripts",
  "hook_event_name": "SessionEnd",
  "reason": "prompt_input_exit"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `reason` | string | Why the session ended (e.g. `"prompt_input_exit"`, `"other"`) |

Note: `prompt_id` may or may not be present depending on whether the session had any turns. SessionEnd does NOT include cost or token data.

## PreCompact

Received immediately before context compaction runs. The hook is synchronous —
compaction waits for it to exit before proceeding, and the on-disk transcript
still holds the full pre-compaction history at this point. (Verified 2026-08-14.)

```json
{
  "session_id": "dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-tmp-x/dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99.jsonl",
  "cwd": "/Users/mturley/tmp/x",
  "prompt_id": "8bd8c7cc-8a7d-4140-819d-37f97ad329aa",
  "hook_event_name": "PreCompact",
  "trigger": "manual",
  "custom_instructions": null
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `trigger` | string | What caused compaction: `"manual"` (user ran `/compact`) or `"auto"` (context limit reached). |
| `custom_instructions` | string \| null | Text argument passed to `/compact` (e.g. `/compact focus on the DB layer`); `null` when none was given or on auto-compaction. |

Note: unlike `PostCompact`, PreCompact fires *before* the transcript is
rewritten, so `transcript_path` still points at the complete pre-compaction
history. agent-handler's PreCompact hook uses this to fork the transcript
(see `handler fork-snapshot`).

## PostCompact

Received after context window compression (auto or manual).

```json
{
  "session_id": "1c7c7bb6-ad30-454e-906e-3084688b1942",
  "transcript_path": "...",
  "cwd": "/Users/mturley/git/rhoai-work",
  "prompt_id": "a7a4e0b8-25c8-4f95-a321-735fa78d05da",
  "hook_event_name": "PostCompact",
  "trigger": "auto",
  "compact_summary": "<analysis>\n...\n</analysis>\n\n<summary>\n...\n</summary>"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `trigger` | string | What caused the compaction (e.g. `"auto"`) |
| `compact_summary` | string | The full compaction summary including analysis and structured summary sections |

The `compact_summary` field contains the complete context summary generated during compaction, wrapped in `<analysis>` and `<summary>` XML tags. This can be very large (several KB).

## StopFailure

Received when a turn ends due to an API error (token limit exceeded, rate limit, billing error, overloaded).

```json
{
  "session_id": "1c7c7bb6-ad30-454e-906e-3084688b1942",
  "transcript_path": "...",
  "cwd": "/Users/mturley/git/rhoai-work",
  "prompt_id": "a7a4e0b8-25c8-4f95-a321-735fa78d05da",
  "agent_id": "ad6313dc7550d4c44",
  "effort": {
    "level": "medium"
  },
  "hook_event_name": "StopFailure",
  "error": "invalid_request",
  "error_details": "400 {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"prompt is too long: 1000841 tokens > 1000000 maximum\"},\"request_id\":\"req_vrtx_...\"}",
  "last_assistant_message": "Prompt is too long"
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `agent_id` | string | Subagent ID if the failure occurred in a subagent |
| `effort.level` | string | Reasoning effort level |
| `error` | string | Error category (e.g. `"invalid_request"`, `"rate_limit"`, `"overloaded"`, `"billing_error"`) |
| `error_details` | string | Full error response from the API including status code and JSON body |
| `last_assistant_message` | string | The assistant's message about the failure |

Note: StopFailure does NOT include cost or token data.

## Not Yet Captured

- **Setup** — fires on first-time configuration. Has not fired during data collection.

## Key Observations

### Cost data is statusline-only
No hook other than the statusline carries cost or token data. Stop, SubagentStop, SessionEnd, and all other lifecycle hooks lack `cost` and `context_window` fields. The statusline hook (firing every ~10s) is the only source.

### Cost resets on restart
The `cost.total_cost_usd` field is in-memory only. When a laptop restarts and the session resumes, this value resets to zero. The `/cost` command in Claude Code recovers the true total from JSONL transcripts, but the statusline hook does not.

### Session name inconsistency
The session name field is called `session_title` in UserPromptSubmit but `session_name` in the statusline hook. Both refer to the display name set via `/name` or auto-generated.

### Model field inconsistency
SessionStart provides `model` as a plain string (e.g. `"claude-opus-4-8[1m]"`). The statusline provides it as an object with `id` and `display_name` fields.

### rate_limits is first-party-API only
The statusline payload carries a `rate_limits` object **only for sessions running against
the first-party Anthropic API**. Sessions on Google Vertex (the work account) omit the key
entirely — everything else in the payload is identical in shape between the two.

```json
"rate_limits": {
  "five_hour": { "used_percentage": 21, "resets_at": 1787790000 },
  "seven_day": { "used_percentage": 12, "resets_at": 1788296400 }
}
```

Both windows are rolling usage quotas; `resets_at` is Unix epoch **seconds**. Treat the whole
object as optional — presence/absence is a reliable signal for which backend a session is on,
but any consumer must handle the missing case.

A related tell: first-party sessions report a bare model id (`claude-opus-5`), while Vertex
sessions on the 1M-context variant report the `[1m]` suffix (`claude-opus-4-8[1m]`,
`claude-sonnet-5[1m]`). Both report `context_window_size: 1000000`, so the suffix — not the
window size — is what distinguishes them.

### prompt_id availability
`prompt_id` is present on most hooks but absent from SessionStart. It is also absent from
statusline payloads on older Claude Code versions (seen missing on 2.1.235, present on
2.1.245/2.1.246) — a version difference, not an account/backend one.

### Tracking a session's cron jobs
Stop and SubagentStop include `session_crons` — an array of the cron jobs active in the
session. Each entry has `id`, `schedule`, `recurring`, and `prompt`. This is a **full
snapshot**, refreshed once per turn, and it is the right source for tracking what a session
currently has scheduled.

The alternative — reconstructing state from `PostToolUse` on `CronCreate`/`CronDelete` —
looks appealing because those events carry the job id and cron expression (see the
PostToolUse section), but it is strictly worse:

- **Auto-deletions emit no event.** A one-shot job fires and is removed from the in-memory
  store with no `CronDelete` hook call; recurring jobs auto-expire after 7 days the same way.
  Verified 2026-08-31: a one-shot scheduled for 10:27 fired on time, and the probe log
  recorded only its `CronCreate` — never a delete. A delta-derived table accumulates ghost
  rows unless the consumer also parses the cron expression and expires rows itself.
- **The snapshot has no such gap.** Jobs removed by any path — explicit delete,
  fire-and-auto-delete, 7-day expiry — are absent from the next `session_crons`.
  Verified 2026-08-31 with a Stop-hook capture across a fire, using a recurring control
  job to prove the array stayed live:

  ```
  11:42:33  cron_count: 2  [f4bfbfcc "0 4 * * *" recurring, 5438be8f "45 11 31 8 *" one-shot]
  11:45:03  cron_count: 1  [f4bfbfcc "0 4 * * *" recurring]
  ```

  The one-shot fired at 11:45 and was gone from the very next snapshot; the control
  remained. No `CronDelete` PostToolUse event accompanied its removal.

Deltas are still useful for *reacting* to the moment a job is created or deleted. For
maintaining a table of what exists, prefer the snapshot.

### scheduled_tasks.lock is not a job-state signal
Scheduling a cron job writes `<cwd>/.claude/scheduled_tasks.lock` (note: in the **project**
directory, not the Claude config dir):

```json
{"sessionId":"54f043a8-...","pid":91959,"procStart":"Thu Aug 27 17:30:58 2026","acquiredAt":1787852029379}
```

It is a first-acquirer lock held until the owning process exits, and it is misleading for
anything else. Verified 2026-08-31:

- **It goes stale.** A session that created a job on Aug 27 still owned the lock on Aug 31,
  while a *different* session created and fired jobs in the same directory without ever
  touching the file.
- **It does not track job existence.** The file survived deletion of the only job that
  caused it to be written.
- **It does not gate firing.** The non-owning session's job fired on schedule while another
  session held the lock.

Use `session_crons` instead.


### Transcript path
The `transcript_path` field provides the full path to the JSONL file without needing to discover it from the filesystem. SubagentStop additionally provides `agent_transcript_path` for the subagent's separate transcript.
