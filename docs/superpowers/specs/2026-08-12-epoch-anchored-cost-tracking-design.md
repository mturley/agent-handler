# Epoch-Anchored Cost Tracking Design

Replace the delta-differencing cost tracker with an epoch-anchored model that uses the Claude process PID to detect restarts, eliminating the recurring double-count and undercount bugs.

## Problem

The current cost tracker records per-tick increments of Claude Code's `total_cost_usd` gauge (from the statusline hook) into a `daily_cost` table. It has had recurring accuracy bugs:

1. **Duplicate adjustments** — concurrent ticks inserting the same reset adjustment (fixed earlier with a transaction).
2. **Reset double-counting** — restart detection via "cost dropped below threshold" fires on Claude Code's non-monotonic recalculation dips, treating a dip as a restart and re-counting the whole session.
3. **Idle drift** — background cost recalculation on idle sessions attributed as phantom spend.
4. **Active-window undercount** — the 60-minute/2-hour "active" gate added to fight idle drift drops real cost from long single-prompt turns.

The root cause is inferring process boundaries from the untrusted cost value. `total_cost_usd` is a cumulative-per-process gauge that is non-monotonic (recalculates, dips) and resets on restart. Differencing it and guessing restarts from its value is fragile.

## Core Model

An **epoch** is one continuous Claude Code process run for a session, identified by the Claude **PID**. Within an epoch, `total_cost_usd` is monotonic and authoritative — it is Claude Code's own number computed from real API calls.

- Cost is tracked by attributing each tick's **within-epoch increment** to the local calendar day.
- A **restart** is detected by a PID change (not by the cost value). On restart, the new process's first observed cost becomes the new epoch's baseline, so recovered-from-transcript cost never double-counts.
- A session's total = `SUM(daily_cost.cost_usd)`, which equals the sum of every epoch's contribution (`peak − baseline`).

This keeps the `daily_cost` mechanism that already works and replaces the two broken parts — cost-drop restart detection and the active-window idle gate — with PID-based epoch detection.

## Data Model

```sql
-- Durable per-day record. Totals = SUM(cost_usd) over rows. Date is LOCAL time.
CREATE TABLE IF NOT EXISTS daily_cost (
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    date TEXT NOT NULL,              -- YYYY-MM-DD in machine local time
    cost_usd REAL NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, date)
);
CREATE INDEX IF NOT EXISTS idx_daily_cost_date ON daily_cost(date);

-- Current epoch reference per session. One row per session, updated in place.
CREATE TABLE IF NOT EXISTS cost_epoch_state (
    session_id TEXT PRIMARY KEY REFERENCES sessions(session_id),
    pid INTEGER NOT NULL,                 -- Claude PID of the current epoch
    last_observed_cost REAL NOT NULL,     -- last reported_cost_usd seen this epoch
    last_observed_input INTEGER NOT NULL,
    last_observed_output INTEGER NOT NULL,
    model TEXT,
    updated_at TEXT NOT NULL
);
```

- `daily_cost` keeps its current shape, so `handler cost`, the web UI, and `SessionTotalCost` continue to work unchanged. **Its `date` column changes from UTC to machine local time.**
- `cost_epoch_state` replaces `cost_snapshots` — a per-session reference row keyed for epoch detection via `pid`.
- `cost_adjustments` and `cost_snapshots` are removed.

## Tick Logic

`RecordCostTick(sessionID string, pid int, model, nowLocal, localDate string, reportedCost float64, reportedInput, reportedOutput int)` runs in one transaction:

```
read cost_epoch_state for session

if no row (first tick ever for this session):
    insert state (pid, last_observed = reported values, model, updated_at)
    attribute nothing to daily_cost      # first observation is the baseline
    commit; return

if pid != state.pid:
    # restart — new epoch. The current reported cost is the new baseline,
    # whether ~0 (fresh start) or recovered-from-transcript (~old peak).
    update state: pid, last_observed = reported values, model, updated_at
    attribute nothing to daily_cost
    commit; return

# same epoch — normal within-epoch increment
costDelta   = reportedCost   - state.last_observed_cost
inputDelta  = reportedInput  - state.last_observed_input
outputDelta = reportedOutput - state.last_observed_output

if costDelta <= 0:
    # dip (recalculation noise) or no change — track the new value,
    # attribute nothing. Next real climb is measured from the dipped value.
    update state.last_observed = reported values, updated_at
    commit; return

update state.last_observed = reported values, model, updated_at
upsert daily_cost[session, localDate] += (costDelta, inputDelta, outputDelta)
commit
```

### Properties

- **PID is the only restart signal.** No inference from the cost value.
- **Within-epoch dips contribute nothing** (`costDelta <= 0`). The next climb is measured from the dipped value — a dip-then-recovery overcounts by the dip amount, which is negligible and rare.
- **Restart with recovered cost never double-counts**: the recovered value is the new baseline; only genuine post-restart spend is attributed.
- **Cross-midnight gaps** attribute the whole increment to the observing tick's local day (accepted minor daily smear; totals stay exact).
- **No idle gate.** Removed. Within-epoch idle drift is small; if it proves material in practice, a light idle gate can be reconsidered later (tracked as a known follow-up, not implemented now).

### Timezone

Day boundaries use the machine's local timezone via Go's `time.Now()` / `time.Local` in the statusline caller, which runs on the user's laptop. No configuration.

## Caller Changes

`recordCostSnapshot` in `cmd/statusline.go`:
- Pass `claudePID()` as the epoch PID.
- Compute `localDate` from `time.Now().Format("2006-01-02")` (local) and `nowLocal` timestamp.
- Remove the `active`/last-prompt window logic and the `costActiveWindow` constant.

## Migration and Cutover

- Add `cost_epoch_state` via `CREATE TABLE IF NOT EXISTS` (schema.sql) and the migration path in `runMigrations`.
- `DROP TABLE IF EXISTS cost_snapshots` in `runMigrations` (replaced by `cost_epoch_state`).
- **Wipe existing `daily_cost` rows at cutover.** They were produced by the buggy logic (both over- and under-counted) and are in UTC, not local. Wiping avoids mixing UTC and local dates and lets data rebuild cleanly. The first tick per session after cutover establishes each epoch's baseline (attributes nothing), so there is no first-tick inflation.

## Not in Scope

- Cost accrued while the statusline is not ticking at all (session fully closed, or cron-only cost with no statusline).
- Non-list-price accuracy (Vertex discounts) — we use Claude Code's local estimate as-is.
- Per-epoch history/audit reporting — only the current epoch reference is stored.
- Re-adding an idle gate — deferred unless real within-epoch drift is observed.

## Testing

`db/cost_test.go`, rewritten for the new signature:

- First tick ever → baseline set, no daily cost.
- Same-PID increase → increment attributed to the local date.
- Same-PID unchanged → nothing attributed.
- Same-PID dip → nothing attributed; reference tracks the dipped value; subsequent climb measured from it.
- PID change, fresh gauge (~0) → new baseline; later climb counted exactly once.
- PID change with recovered cost (gauge jumps to old peak) → new baseline = recovered value; no double-count.
- Increments land on the correct local date across a day boundary.
- `SessionTotalCost` = sum across days = sum of epoch contributions.

## Files Touched

| File | Change |
|------|--------|
| `db/schema.sql` | Add `cost_epoch_state`; remove `cost_snapshots` |
| `db/db.go` | Migration: create `cost_epoch_state`, drop `cost_snapshots` |
| `db/cost.go` | Replace `RecordCostTick`; replace `CostSnapshot`/snapshot helpers with epoch-state read/write; keep `SessionTotalCost` and daily query helpers |
| `db/cost_test.go` | Rewrite tests for the new model |
| `cmd/statusline.go` | `recordCostSnapshot` passes PID + local date; drop active window |
| `cmd/cost.go`, `cmd/api/*`, `ui/*` | No change (read `daily_cost` / `SessionTotalCost`) |
