# Claude Code Hook Stdin Data

Claude Code passes JSON on stdin to all hook scripts. The available fields vary by hook type — lifecycle hooks (SessionStart, UserPromptSubmit) receive a minimal set, while the statusline hook receives a rich session snapshot.

All data captured from Claude Code version 2.1.198–2.1.201 (July 2026). Field availability may change across versions.

## Common Fields (All Hooks)

Every hook receives at least these fields:

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Claude session UUID — the primary identifier |
| `transcript_path` | string | Absolute path to the session's JSONL transcript file |
| `cwd` | string | Current working directory |
| `prompt_id` | string | UUID of the current prompt turn |

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
| `permission_mode` | string | Current permission mode (`"default"`, `"acceptEdits"`, `"plan"`, etc.) |
| `hook_event_name` | string | Always `"UserPromptSubmit"` |
| `prompt` | string | The user's prompt text |
| `session_title` | string | Display name of the session (set via `/name` or auto-generated) |

## Statusline

Received on each statusline refresh (configurable interval, default 10s). Contains the richest data set.

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
| `cost.total_cost_usd` | number | Total API cost for the session |
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
| `pr.number` | number | Associated PR number (if session is linked to a PR) |
| `pr.url` | string | PR URL |
| `pr.review_state` | string | PR review state (`"pending"`, etc.) |

## Key Observations

### Session ID naming inconsistency
The session name field is called `session_title` in UserPromptSubmit but `session_name` in the statusline hook. Both refer to the display name set via `/name` or auto-generated.

### Session ID reliability
The `session_id` from stdin is the authoritative session identifier. Previous versions of agent-handler discovered the session ID by finding the most recently modified JSONL file in the project directory — this fails when multiple sessions share the same project directory. Always prefer the stdin `session_id`.

### Transcript path
The `transcript_path` field provides the full path to the JSONL file without needing to discover it from the filesystem. This is more reliable than the `find_project_dir` + `ls -t` approach.

### Statusline data opportunities
The statusline's rich data (cost, context window, model, PR info) could be used to enhance handler's session status display — showing context usage, cost, and associated PRs without any additional API calls.
