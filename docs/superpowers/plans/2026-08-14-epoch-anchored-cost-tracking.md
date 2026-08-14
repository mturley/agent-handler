# Epoch-Anchored Cost Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the delta-differencing cost tracker with an epoch-anchored model that detects Claude Code restarts by PID, eliminating the reset-double-count and active-window-undercount bugs.

**Architecture:** An epoch is one continuous Claude Code process run, identified by the Claude PID. Within an epoch `total_cost_usd` is monotonic; each tick's within-epoch increment is attributed to the local calendar day in `daily_cost`. A PID change starts a new epoch whose baseline is the first observed cost, so recovered-from-transcript cost never double-counts. Per-session epoch reference state lives in a new `cost_epoch_state` table replacing `cost_snapshots`.

**Tech Stack:** Go 1.25, SQLite (modernc.org/sqlite), cobra CLI.

## Global Constraints

- `daily_cost.date` is the **machine local date** (`time.Now()` local), not UTC.
- Restart detection uses the **Claude PID only** — never the cost value.
- An epoch attributes to `daily_cost` only its within-epoch increments; the first tick of any epoch (including the first ever for a session) establishes a baseline and attributes nothing.
- Within-epoch cost dips (`costDelta <= 0`) attribute nothing and do not create a daily_cost row.
- All timestamps stored in state rows are ISO 8601 (`time.RFC3339`).
- `cost_snapshots` and `cost_adjustments` tables are removed; `daily_cost` keeps its existing shape.
- Existing `daily_cost` rows are wiped at cutover (they mix UTC dates and buggy values).

See the design spec: `docs/superpowers/specs/2026-08-12-epoch-anchored-cost-tracking-design.md`.

## Notes

The authoritative task detail lives in the extracted brief for each task under
`.superpowers/sdd/2026-08-14-epoch-anchored-cost-tracking/`. This file records
the task boundaries and global constraints; the implementer works from its brief.

### Task 1: Schema and epoch-state DB layer
Replace `cost_snapshots` + cost-drop reset detection with `cost_epoch_state` and PID-based `RecordCostTick`. Keep `daily_cost` and its query helpers. Migration creates `cost_epoch_state`, drops `cost_snapshots`, wipes `daily_cost`. Files: `db/schema.sql`, `db/db.go`, `db/cost.go`, `db/cost_test.go`, `db/db_test.go`.

### Task 2: Statusline caller
`recordCostSnapshot` in `cmd/statusline.go` passes `claudePID()` and the machine-local date; remove the active-window logic and `costActiveWindow`.

### Task 3: handler cost --session accessor swap
`cmd/cost.go` uses `GetCostEpochState` instead of `GetCostSnapshot` for reported cost and model.

### Task 4: Install and verify end-to-end
`NONINTERACTIVE=1 make install`; verify migration dropped `cost_snapshots`, created `cost_epoch_state`, wiped `daily_cost`; confirm cost accrues under real ticks.
