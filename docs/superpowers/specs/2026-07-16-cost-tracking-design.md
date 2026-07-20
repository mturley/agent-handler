# Cost Tracking Design

Track per-session and aggregate Claude Code API costs in agent-handler, enabling monthly/daily/per-session spend breakdowns via `handler cost` and real-time cost display in the statusline.

## Problem

1. Claude Code's `cost.total_cost_usd` (from the statusline hook stdin) is cumulative per session but resets when the laptop restarts and the session resumes. The `/cost` command recovers the true total from JSONL transcripts, but the statusline hook does not.
2. Cost is per-session with no built-in cross-session aggregation. There is no way to answer "how much did I spend this month?" without manual work.
3. JSONL transcript parsing is unreliable for cost reconstruction (double-counting, rotation issues observed in practice).
4. The user is on Vertex, so Anthropic billing dashboards and Admin APIs are unavailable.

## Data Source

The statusline hook is the only reliable source. It fires every ~10 seconds and provides (via stdin JSON):

| Field | Type | Description |
|-------|------|-------------|
| `cost.total_cost_usd` | float | Cumulative session cost (in-memory, resets on restart) |
| `cost.total_duration_ms` | int | Total wall-clock time |
| `cost.total_api_duration_ms` | int | Total API call time |
| `context_window.total_input_tokens` | int | Cumulative input tokens |
| `context_window.total_output_tokens` | int | Cumulative output tokens |
| `model.id` | string | Model identifier (e.g. `claude-opus-4-6[1m]`) |

No other hook (Stop, SubagentStop, SessionEnd, etc.) carries cost or token data — confirmed by capturing stdin from all hook event types.

## Approach: Delta Snapshots with Daily Rollups

Each statusline tick compares the reported cost against the last known value for that session. If it changed, compute the delta and attribute it to today's date. If the reported value is lower than the last known value, a restart reset occurred — record the lost amount as an adjustment.

### Schema

Three new tables:

```sql
CREATE TABLE IF NOT EXISTS cost_snapshots (
    session_id TEXT PRIMARY KEY REFERENCES sessions(session_id),
    reported_cost_usd REAL NOT NULL,
    total_input_tokens INTEGER NOT NULL,
    total_output_tokens INTEGER NOT NULL,
    model TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cost_adjustments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    adjustment_usd REAL NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS daily_cost (
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    date TEXT NOT NULL,
    cost_usd REAL NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, date)
);

CREATE INDEX IF NOT EXISTS idx_daily_cost_date ON daily_cost(date);
```

- `cost_snapshots`: One row per session, updated in-place. Scratchpad for computing deltas.
- `cost_adjustments`: Append-only log of restart resets. Each row records cost lost by a reset.
- `daily_cost`: One row per (session, date). Accumulates deltas attributed to each UTC day.

### Tick Logic

On each statusline tick (in the existing writable DB section of `runStatuslineFromHook`):

1. Read `cost_snapshots` for this `session_id`.
2. If no snapshot exists:
   - INSERT snapshot with current `reported_cost_usd`, tokens, model.
   - UPSERT `daily_cost` += `reported_cost_usd` for today (first tick captures existing cost).
   - Done.
3. If `reported_cost_usd == snapshot.reported_cost_usd`:
   - Skip — no writes.
   - Done.
4. If `reported_cost_usd < snapshot.reported_cost_usd`:
   - Reset detected.
   - INSERT into `cost_adjustments` with `adjustment_usd = snapshot.reported_cost_usd`, reason `'restart_reset'`.
   - `delta = reported_cost_usd` (the new cost accumulated since reset).
5. Else (normal increase):
   - `delta = reported_cost_usd - snapshot.reported_cost_usd`.
6. UPDATE `cost_snapshots` with new values.
7. Compute token deltas the same way as cost: on reset, `token_delta = current_tokens`; otherwise `token_delta = current_tokens - snapshot_tokens`. UPSERT `daily_cost`: `cost_usd += delta`, `input_tokens += input_token_delta`, `output_tokens += output_token_delta` for today's date.

Write optimization: step 3 skips all DB writes when cost hasn't changed, which is the common case during idle periods.

### True Session Cost

The true lifetime cost of a session is:

```
true_cost = current_reported_cost_usd + SUM(cost_adjustments.adjustment_usd)
```

This handles any number of restart resets.

## `hookInput` Struct Changes

Expand the existing struct to capture token data and model ID:

```go
type hookInput struct {
    // ... existing fields ...
    Model struct {
        ID          string `json:"id"`
        DisplayName string `json:"display_name"`
    } `json:"model"`
    ContextWindow struct {
        UsedPercentage    int `json:"used_percentage"`
        TotalInputTokens  int `json:"total_input_tokens"`
        TotalOutputTokens int `json:"total_output_tokens"`
    } `json:"context_window"`
    Cost struct {
        TotalCostUSD float64 `json:"total_cost_usd"`
    } `json:"cost"`
}
```

The `Model.ID` field is already parsed for `DisplayName`; adding `ID` is a one-line change.

## CLI: `handler cost`

New subcommand with flags:

```
handler cost                    # current month summary
handler cost --month 2026-06    # specific month
handler cost --today            # today only
handler cost --session <id>     # single session detail
handler cost --json             # machine-readable output
```

### Default output (current month)

```
July 2026: $342.17
  22 sessions | 12.4M input tokens | 1.1M output tokens

  By day:
    Jul 16  $48.23  (6 sessions)
    Jul 15  $31.07  (4 sessions)
    Jul 14  $52.91  (8 sessions)
    ...

  Top sessions:
    vllm-configs-admin     $67.12
    investigate-kserve     $43.88
    cost-tracking          $12.34
    ...
```

### `--today` output

```
Today (Jul 16): $48.23
  6 sessions | 1.8M input tokens | 203K output tokens

  By session:
    vllm-configs-admin     $18.42
    investigate-kserve     $14.20
    cost-tracking           $8.91
    ...
```

### Queries

Monthly total: `SELECT SUM(cost_usd) FROM daily_cost WHERE date BETWEEN '2026-07-01' AND '2026-07-31'`

Daily breakdown: `SELECT date, SUM(cost_usd), COUNT(DISTINCT session_id) FROM daily_cost WHERE date BETWEEN ... GROUP BY date ORDER BY date DESC`

Top sessions for a period: `SELECT session_id, SUM(cost_usd) FROM daily_cost WHERE date BETWEEN ... GROUP BY session_id ORDER BY SUM(cost_usd) DESC`

Session true cost: `SELECT reported_cost_usd FROM cost_snapshots WHERE session_id = ? UNION SELECT SUM(adjustment_usd) FROM cost_adjustments WHERE session_id = ?`

## Statusline Enhancements

### Worker sessions

Model line changes from:
```
Opus 4.6 (1M context) ▓▓▓▓▓░░░░░░░░░░░░░░░ 28% ctx | $39.07
```
to:
```
Opus 4.6 (1M context) ▓▓▓▓▓░░░░░░░░░░░░░░░ 28% ctx | $39.07 ($18.42 today)
```

Where `$39.07` is the true session cost (reported + adjustments) and `$18.42` is this session's `daily_cost` for today.

### Handler session

Gets the same per-session cost on the model line, plus an aggregate cost line:
```
Opus 4.6 (1M context) ▓▓▓▓▓░░░░░░░░░░░░░░░ 28% ctx | $12.34 ($8.91 today)
[Handler] Sessions: 6 active, 1 blocked — /handler to summarize all sessions
Cost: $48.23 today | $342.17 this month
```

The aggregate line sums `daily_cost` across all sessions for today and for the current month.

## Performance

- The statusline hook must render quickly (~100ms budget). All cost tracking writes happen in the existing "brief writable connection" section before rendering begins.
- Writes are skipped entirely when `reported_cost_usd` hasn't changed (the common case during idle).
- All queries for the statusline display (session today cost, aggregate today/month) are simple indexed lookups on `daily_cost`.
- The `handler cost` CLI command uses read-only queries with no performance constraints.

## File Changes

| File | Change |
|------|--------|
| `db/schema.sql` | Add 3 new tables |
| `db/db.go` | No migration needed (new tables use CREATE IF NOT EXISTS) |
| `db/cost.go` | New file: snapshot read/upsert, adjustment insert, daily_cost upsert, query helpers |
| `cmd/statusline.go` | Expand `hookInput` struct, add cost snapshot logic to writable section, update `formatModelLine` and `renderHandlerStatusline` |
| `cmd/cost.go` | New file: `handler cost` subcommand |

## Not in Scope

- Per-model cost breakdown (we track model ID but don't split cost by model — Claude Code's `total_cost_usd` is already aggregated).
- Cost alerting or budget limits.
- Backfilling historical cost from JSONL transcripts (unreliable, as established).
- Token-based cost estimation (we use Claude Code's own `total_cost_usd` rather than computing from token counts and price tables).
