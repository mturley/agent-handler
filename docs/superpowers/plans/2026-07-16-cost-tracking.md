# Cost Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track per-session API costs with reset detection, daily rollups, and a `handler cost` CLI command for monthly/daily/per-session spend breakdowns.

**Architecture:** The statusline hook (fires every ~10s) captures `cost.total_cost_usd` from Claude Code's stdin JSON. On each tick, the handler compares with the last-known value, computes a delta, detects resets (value dropped after laptop restart), and accumulates cost into a `daily_cost` table keyed by (session, date). The `handler cost` command queries this table for aggregated views. The statusline is enhanced to show true session cost and daily spend.

**Tech Stack:** Go, SQLite (via modernc.org/sqlite), cobra CLI

## Global Constraints

- All timestamps are ISO 8601 UTC.
- New tables use `CREATE TABLE IF NOT EXISTS` in `schema.sql` — no migration needed.
- The `db_test.go` table count assertion must be updated when adding tables.
- DB writes in the statusline hook must be fast (sub-millisecond) — they run in the existing "brief writable connection" section before rendering.
- Skip writes entirely when `reported_cost_usd` hasn't changed since last snapshot.

---

### Task 1: Schema and DB Layer (`db/cost.go`)

Add the three cost tables to the schema and implement all DB access functions.

**Files:**
- Modify: `db/schema.sql` — append 3 new tables + 1 index
- Create: `db/cost.go` — structs + all DB functions
- Create: `db/cost_test.go` — tests for every DB function
- Modify: `db/db_test.go:31-52` — add new tables to the `expectedTables` slice

**Interfaces:**
- Produces:
  - `CostSnapshot` struct: `SessionID string`, `ReportedCostUSD float64`, `TotalInputTokens int`, `TotalOutputTokens int`, `Model string`, `UpdatedAt string`
  - `DailyCost` struct: `SessionID string`, `Date string`, `CostUSD float64`, `InputTokens int`, `OutputTokens int`
  - `func (db *DB) GetCostSnapshot(sessionID string) (*CostSnapshot, error)` — returns nil, nil if not found
  - `func (db *DB) UpsertCostSnapshot(s CostSnapshot) error`
  - `func (db *DB) InsertCostAdjustment(sessionID string, adjustmentUSD float64, reason string, createdAt string) error`
  - `func (db *DB) GetTotalAdjustment(sessionID string) (float64, error)` — returns SUM of adjustments
  - `func (db *DB) UpsertDailyCost(sessionID, date string, costDelta float64, inputTokensDelta, outputTokensDelta int) error` — adds deltas to existing row or creates new
  - `func (db *DB) GetDailyCostForSession(sessionID, date string) (*DailyCost, error)` — single session+date
  - `func (db *DB) QueryDailyCostByDate(startDate, endDate string) ([]DateSummary, error)` — grouped by date
  - `func (db *DB) QueryDailyCostBySession(startDate, endDate string) ([]SessionSummary, error)` — grouped by session, includes session_name via JOIN
  - `func (db *DB) QueryTotalCost(startDate, endDate string) (float64, int, int, error)` — total cost, input tokens, output tokens
  - `DateSummary` struct: `Date string`, `CostUSD float64`, `SessionCount int`
  - `SessionSummary` struct: `SessionID string`, `SessionName string`, `CostUSD float64`, `InputTokens int`, `OutputTokens int`

- [ ] **Step 1: Add tables to schema.sql**

Append to the end of `db/schema.sql`:

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

- [ ] **Step 2: Update db_test.go table count**

In `db/db_test.go`, add the 3 new table names to the `expectedTables` slice:

```go
expectedTables := []string{
    "events",
    "event_recipients",
    "event_resources",
    "sessions",
    "session_cursors",
    "subscriptions",
    "resource_relationships",
    "resource_state",
    "cost_snapshots",
    "cost_adjustments",
    "daily_cost",
}
```

- [ ] **Step 3: Write cost.go with structs and all DB functions**

Create `db/cost.go`:

```go
package db

import (
	"database/sql"
	"fmt"
)

type CostSnapshot struct {
	SessionID        string
	ReportedCostUSD  float64
	TotalInputTokens int
	TotalOutputTokens int
	Model            string
	UpdatedAt        string
}

type DailyCost struct {
	SessionID    string
	Date         string
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

type DateSummary struct {
	Date         string
	CostUSD      float64
	SessionCount int
}

type SessionSummary struct {
	SessionID    string
	SessionName  string
	CostUSD      float64
	InputTokens  int
	OutputTokens int
}

func (db *DB) GetCostSnapshot(sessionID string) (*CostSnapshot, error) {
	var s CostSnapshot
	err := db.conn.QueryRow(`
		SELECT session_id, reported_cost_usd, total_input_tokens, total_output_tokens, model, updated_at
		FROM cost_snapshots WHERE session_id = ?
	`, sessionID).Scan(&s.SessionID, &s.ReportedCostUSD, &s.TotalInputTokens, &s.TotalOutputTokens, &s.Model, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cost snapshot: %w", err)
	}
	return &s, nil
}

func (db *DB) UpsertCostSnapshot(s CostSnapshot) error {
	_, err := db.conn.Exec(`
		INSERT INTO cost_snapshots (session_id, reported_cost_usd, total_input_tokens, total_output_tokens, model, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			reported_cost_usd = excluded.reported_cost_usd,
			total_input_tokens = excluded.total_input_tokens,
			total_output_tokens = excluded.total_output_tokens,
			model = excluded.model,
			updated_at = excluded.updated_at
	`, s.SessionID, s.ReportedCostUSD, s.TotalInputTokens, s.TotalOutputTokens, s.Model, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert cost snapshot: %w", err)
	}
	return nil
}

func (db *DB) InsertCostAdjustment(sessionID string, adjustmentUSD float64, reason, createdAt string) error {
	_, err := db.conn.Exec(`
		INSERT INTO cost_adjustments (session_id, adjustment_usd, reason, created_at)
		VALUES (?, ?, ?, ?)
	`, sessionID, adjustmentUSD, reason, createdAt)
	if err != nil {
		return fmt.Errorf("failed to insert cost adjustment: %w", err)
	}
	return nil
}

func (db *DB) GetTotalAdjustment(sessionID string) (float64, error) {
	var total sql.NullFloat64
	err := db.conn.QueryRow(`
		SELECT SUM(adjustment_usd) FROM cost_adjustments WHERE session_id = ?
	`, sessionID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get total adjustment: %w", err)
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

func (db *DB) UpsertDailyCost(sessionID, date string, costDelta float64, inputTokensDelta, outputTokensDelta int) error {
	_, err := db.conn.Exec(`
		INSERT INTO daily_cost (session_id, date, cost_usd, input_tokens, output_tokens)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id, date) DO UPDATE SET
			cost_usd = daily_cost.cost_usd + excluded.cost_usd,
			input_tokens = daily_cost.input_tokens + excluded.input_tokens,
			output_tokens = daily_cost.output_tokens + excluded.output_tokens
	`, sessionID, date, costDelta, inputTokensDelta, outputTokensDelta)
	if err != nil {
		return fmt.Errorf("failed to upsert daily cost: %w", err)
	}
	return nil
}

func (db *DB) GetDailyCostForSession(sessionID, date string) (*DailyCost, error) {
	var dc DailyCost
	err := db.conn.QueryRow(`
		SELECT session_id, date, cost_usd, input_tokens, output_tokens
		FROM daily_cost WHERE session_id = ? AND date = ?
	`, sessionID, date).Scan(&dc.SessionID, &dc.Date, &dc.CostUSD, &dc.InputTokens, &dc.OutputTokens)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get daily cost: %w", err)
	}
	return &dc, nil
}

func (db *DB) QueryDailyCostByDate(startDate, endDate string) ([]DateSummary, error) {
	rows, err := db.conn.Query(`
		SELECT date, SUM(cost_usd), COUNT(DISTINCT session_id)
		FROM daily_cost
		WHERE date >= ? AND date <= ?
		GROUP BY date
		ORDER BY date DESC
	`, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily cost by date: %w", err)
	}
	defer rows.Close()

	var results []DateSummary
	for rows.Next() {
		var ds DateSummary
		if err := rows.Scan(&ds.Date, &ds.CostUSD, &ds.SessionCount); err != nil {
			return nil, fmt.Errorf("failed to scan date summary: %w", err)
		}
		results = append(results, ds)
	}
	return results, rows.Err()
}

func (db *DB) QueryDailyCostBySession(startDate, endDate string) ([]SessionSummary, error) {
	rows, err := db.conn.Query(`
		SELECT dc.session_id, COALESCE(s.session_name, ''), SUM(dc.cost_usd), SUM(dc.input_tokens), SUM(dc.output_tokens)
		FROM daily_cost dc
		LEFT JOIN sessions s ON s.session_id = dc.session_id
		WHERE dc.date >= ? AND dc.date <= ?
		GROUP BY dc.session_id
		ORDER BY SUM(dc.cost_usd) DESC
	`, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily cost by session: %w", err)
	}
	defer rows.Close()

	var results []SessionSummary
	for rows.Next() {
		var ss SessionSummary
		if err := rows.Scan(&ss.SessionID, &ss.SessionName, &ss.CostUSD, &ss.InputTokens, &ss.OutputTokens); err != nil {
			return nil, fmt.Errorf("failed to scan session summary: %w", err)
		}
		results = append(results, ss)
	}
	return results, rows.Err()
}

func (db *DB) QueryTotalCost(startDate, endDate string) (float64, int, int, error) {
	var cost sql.NullFloat64
	var inputTokens, outputTokens sql.NullInt64
	err := db.conn.QueryRow(`
		SELECT SUM(cost_usd), SUM(input_tokens), SUM(output_tokens)
		FROM daily_cost
		WHERE date >= ? AND date <= ?
	`, startDate, endDate).Scan(&cost, &inputTokens, &outputTokens)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to query total cost: %w", err)
	}
	c := 0.0
	if cost.Valid {
		c = cost.Float64
	}
	it := 0
	if inputTokens.Valid {
		it = int(inputTokens.Int64)
	}
	ot := 0
	if outputTokens.Valid {
		ot = int(outputTokens.Int64)
	}
	return c, it, ot, nil
}
```

- [ ] **Step 4: Write cost_test.go**

Create `db/cost_test.go`:

```go
package db

import "testing"

func TestGetCostSnapshotNotFound(t *testing.T) {
	d := testDB(t)
	snap, err := d.GetCostSnapshot("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestUpsertAndGetCostSnapshot(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cost-test-1")

	s := CostSnapshot{
		SessionID:         "cost-test-1",
		ReportedCostUSD:   12.50,
		TotalInputTokens:  100000,
		TotalOutputTokens: 5000,
		Model:             "claude-opus-4-6[1m]",
		UpdatedAt:         "2026-07-16T10:00:00Z",
	}
	if err := d.UpsertCostSnapshot(s); err != nil {
		t.Fatalf("UpsertCostSnapshot failed: %v", err)
	}

	got, err := d.GetCostSnapshot("cost-test-1")
	if err != nil {
		t.Fatalf("GetCostSnapshot failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if got.ReportedCostUSD != 12.50 {
		t.Errorf("expected cost 12.50, got %f", got.ReportedCostUSD)
	}
	if got.TotalInputTokens != 100000 {
		t.Errorf("expected 100000 input tokens, got %d", got.TotalInputTokens)
	}
	if got.Model != "claude-opus-4-6[1m]" {
		t.Errorf("expected model claude-opus-4-6[1m], got %s", got.Model)
	}
}

func TestUpsertCostSnapshotOverwrites(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cost-test-2")

	d.UpsertCostSnapshot(CostSnapshot{
		SessionID: "cost-test-2", ReportedCostUSD: 5.00,
		TotalInputTokens: 50000, TotalOutputTokens: 2000,
		Model: "claude-opus-4-6[1m]", UpdatedAt: "2026-07-16T10:00:00Z",
	})
	d.UpsertCostSnapshot(CostSnapshot{
		SessionID: "cost-test-2", ReportedCostUSD: 10.00,
		TotalInputTokens: 100000, TotalOutputTokens: 4000,
		Model: "claude-opus-4-6[1m]", UpdatedAt: "2026-07-16T10:05:00Z",
	})

	got, _ := d.GetCostSnapshot("cost-test-2")
	if got.ReportedCostUSD != 10.00 {
		t.Errorf("expected 10.00, got %f", got.ReportedCostUSD)
	}
}

func TestInsertCostAdjustmentAndGetTotal(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "adj-test")

	d.InsertCostAdjustment("adj-test", 25.00, "restart_reset", "2026-07-16T10:00:00Z")
	d.InsertCostAdjustment("adj-test", 15.00, "restart_reset", "2026-07-16T14:00:00Z")

	total, err := d.GetTotalAdjustment("adj-test")
	if err != nil {
		t.Fatalf("GetTotalAdjustment failed: %v", err)
	}
	if total != 40.00 {
		t.Errorf("expected 40.00, got %f", total)
	}
}

func TestGetTotalAdjustmentNoRows(t *testing.T) {
	d := testDB(t)
	total, err := d.GetTotalAdjustment("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got %f", total)
	}
}

func TestUpsertDailyCostAccumulates(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "daily-test")

	d.UpsertDailyCost("daily-test", "2026-07-16", 5.00, 50000, 2000)
	d.UpsertDailyCost("daily-test", "2026-07-16", 3.00, 30000, 1000)

	dc, err := d.GetDailyCostForSession("daily-test", "2026-07-16")
	if err != nil {
		t.Fatalf("GetDailyCostForSession failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil daily cost")
	}
	if dc.CostUSD != 8.00 {
		t.Errorf("expected 8.00, got %f", dc.CostUSD)
	}
	if dc.InputTokens != 80000 {
		t.Errorf("expected 80000 input tokens, got %d", dc.InputTokens)
	}
	if dc.OutputTokens != 3000 {
		t.Errorf("expected 3000 output tokens, got %d", dc.OutputTokens)
	}
}

func TestQueryDailyCostByDate(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "date-q-1")
	seedSession(t, d, "date-q-2")

	d.UpsertDailyCost("date-q-1", "2026-07-15", 10.00, 100000, 5000)
	d.UpsertDailyCost("date-q-2", "2026-07-15", 8.00, 80000, 4000)
	d.UpsertDailyCost("date-q-1", "2026-07-16", 12.00, 120000, 6000)

	results, err := d.QueryDailyCostByDate("2026-07-15", "2026-07-16")
	if err != nil {
		t.Fatalf("QueryDailyCostByDate failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 dates, got %d", len(results))
	}
	// Results ordered DESC, so Jul 16 first
	if results[0].Date != "2026-07-16" {
		t.Errorf("expected first date 2026-07-16, got %s", results[0].Date)
	}
	if results[0].CostUSD != 12.00 {
		t.Errorf("expected 12.00 for Jul 16, got %f", results[0].CostUSD)
	}
	if results[1].SessionCount != 2 {
		t.Errorf("expected 2 sessions for Jul 15, got %d", results[1].SessionCount)
	}
}

func TestQueryDailyCostBySession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-q-1")
	seedSession(t, d, "sess-q-2")

	// Give sess-q-1 a name
	d.conn.Exec(`UPDATE sessions SET session_name = 'my-session' WHERE session_id = 'sess-q-1'`)

	d.UpsertDailyCost("sess-q-1", "2026-07-15", 10.00, 100000, 5000)
	d.UpsertDailyCost("sess-q-1", "2026-07-16", 12.00, 120000, 6000)
	d.UpsertDailyCost("sess-q-2", "2026-07-16", 8.00, 80000, 4000)

	results, err := d.QueryDailyCostBySession("2026-07-15", "2026-07-16")
	if err != nil {
		t.Fatalf("QueryDailyCostBySession failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(results))
	}
	// Ordered by cost DESC, so sess-q-1 (22.00) first
	if results[0].SessionName != "my-session" {
		t.Errorf("expected session name 'my-session', got %q", results[0].SessionName)
	}
	if results[0].CostUSD != 22.00 {
		t.Errorf("expected 22.00, got %f", results[0].CostUSD)
	}
}

func TestQueryTotalCost(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "total-q-1")
	seedSession(t, d, "total-q-2")

	d.UpsertDailyCost("total-q-1", "2026-07-15", 10.00, 100000, 5000)
	d.UpsertDailyCost("total-q-2", "2026-07-16", 8.00, 80000, 4000)

	cost, input, output, err := d.QueryTotalCost("2026-07-15", "2026-07-16")
	if err != nil {
		t.Fatalf("QueryTotalCost failed: %v", err)
	}
	if cost != 18.00 {
		t.Errorf("expected total cost 18.00, got %f", cost)
	}
	if input != 180000 {
		t.Errorf("expected 180000 input tokens, got %d", input)
	}
	if output != 9000 {
		t.Errorf("expected 9000 output tokens, got %d", output)
	}
}

func TestQueryTotalCostEmpty(t *testing.T) {
	d := testDB(t)
	cost, input, output, err := d.QueryTotalCost("2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 0 || input != 0 || output != 0 {
		t.Errorf("expected all zeros, got cost=%f input=%d output=%d", cost, input, output)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./db/ -v -run "TestGetCostSnapshot|TestUpsertAndGetCostSnapshot|TestUpsertCostSnapshot|TestInsertCostAdjustment|TestGetTotalAdjustment|TestUpsertDailyCost|TestQueryDailyCost|TestQueryTotalCost|TestOpen"`

Expected: All PASS. The `TestOpen` test should now expect 11 tables.

- [ ] **Step 6: Commit**

```bash
git add db/schema.sql db/cost.go db/cost_test.go db/db_test.go
git commit --signoff -m "feat: add cost tracking schema and DB layer

Add cost_snapshots, cost_adjustments, and daily_cost tables with
full CRUD functions for tracking session API costs with reset
detection and daily rollups."
```

---

### Task 2: Statusline Cost Capture

Wire the cost snapshot logic into the statusline hook's existing writable DB section. Expand the `hookInput` struct to parse token and model data.

**Files:**
- Modify: `cmd/statusline.go:57-72` — expand `hookInput` struct
- Modify: `cmd/statusline.go:100-108` — add cost snapshot logic to writable section

**Interfaces:**
- Consumes: `db.GetCostSnapshot`, `db.UpsertCostSnapshot`, `db.InsertCostAdjustment`, `db.UpsertDailyCost`, `db.CostSnapshot` (from Task 1)
- Produces: Cost data populated in the DB on every statusline tick where cost changed

- [ ] **Step 1: Expand hookInput struct**

In `cmd/statusline.go`, replace the `hookInput` struct (lines 58-72) with:

```go
type hookInput struct {
	SessionID      string `json:"session_id"`
	SessionName    string `json:"session_name"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          struct {
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

- [ ] **Step 2: Add cost snapshot function**

Add a new function to `cmd/statusline.go`:

```go
func recordCostSnapshot(wd *db.DB, input *hookInput) {
	now := time.Now().UTC().Format(time.RFC3339)
	today := time.Now().UTC().Format("2006-01-02")
	sessionID := input.SessionID
	reportedCost := input.Cost.TotalCostUSD
	reportedInput := input.ContextWindow.TotalInputTokens
	reportedOutput := input.ContextWindow.TotalOutputTokens

	snap, err := wd.GetCostSnapshot(sessionID)
	if err != nil {
		return
	}

	if snap == nil {
		wd.UpsertCostSnapshot(db.CostSnapshot{
			SessionID:         sessionID,
			ReportedCostUSD:   reportedCost,
			TotalInputTokens:  reportedInput,
			TotalOutputTokens: reportedOutput,
			Model:             input.Model.ID,
			UpdatedAt:         now,
		})
		if reportedCost > 0 {
			wd.UpsertDailyCost(sessionID, today, reportedCost, reportedInput, reportedOutput)
		}
		return
	}

	if reportedCost == snap.ReportedCostUSD {
		return
	}

	var costDelta float64
	var inputDelta, outputDelta int

	if reportedCost < snap.ReportedCostUSD {
		wd.InsertCostAdjustment(sessionID, snap.ReportedCostUSD, "restart_reset", now)
		costDelta = reportedCost
		inputDelta = reportedInput
		outputDelta = reportedOutput
	} else {
		costDelta = reportedCost - snap.ReportedCostUSD
		inputDelta = reportedInput - snap.TotalInputTokens
		outputDelta = reportedOutput - snap.TotalOutputTokens
	}

	wd.UpsertCostSnapshot(db.CostSnapshot{
		SessionID:         sessionID,
		ReportedCostUSD:   reportedCost,
		TotalInputTokens:  reportedInput,
		TotalOutputTokens: reportedOutput,
		Model:             input.Model.ID,
		UpdatedAt:         now,
	})
	if costDelta > 0 {
		wd.UpsertDailyCost(sessionID, today, costDelta, inputDelta, outputDelta)
	}
}
```

- [ ] **Step 3: Call recordCostSnapshot in the writable section**

In `cmd/statusline.go`, modify the writable DB section (around line 101-108). Change:

```go
	// Brief writable connection for heartbeat, then read-only for rendering
	if wd, err := openDB(); err == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		wd.BumpLastActive(input.SessionID, now)
		termType, termID, workspaceID := terminal.Detect()
		syncSessionMetadata(wd, input.SessionID, input.SessionName, claudePID(), termType, termID, workspaceID)
		wd.Close()
	}
```

to:

```go
	// Brief writable connection for heartbeat + cost tracking, then read-only for rendering
	if wd, err := openDB(); err == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		wd.BumpLastActive(input.SessionID, now)
		termType, termID, workspaceID := terminal.Detect()
		syncSessionMetadata(wd, input.SessionID, input.SessionName, claudePID(), termType, termID, workspaceID)
		recordCostSnapshot(wd, &input)
		wd.Close()
	}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`

Expected: Compiles with no errors.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`

Expected: All PASS.

- [ ] **Step 6: Manual verification**

After building and installing (`NONINTERACTIVE=1 make install`), use Claude in another session and watch the database:

Run: `handler query "SELECT * FROM cost_snapshots LIMIT 5"`
Run: `handler query "SELECT * FROM daily_cost ORDER BY date DESC LIMIT 10"`

Expected: Rows appear as sessions accumulate cost.

- [ ] **Step 7: Commit**

```bash
git add cmd/statusline.go
git commit --signoff -m "feat: capture cost snapshots from statusline hook

Record cost deltas on each statusline tick with reset detection.
Skips writes when cost hasn't changed."
```

---

### Task 3: `handler cost` CLI Command

Implement the `handler cost` subcommand with `--month`, `--today`, `--session`, and `--json` flags.

**Files:**
- Create: `cmd/cost.go` — cobra command with all flags and rendering

**Interfaces:**
- Consumes: `db.QueryDailyCostByDate`, `db.QueryDailyCostBySession`, `db.QueryTotalCost`, `db.GetCostSnapshot`, `db.GetTotalAdjustment`, `db.GetDailyCostForSession` (from Task 1), `db.GetSession` (existing)
- Produces: `handler cost` CLI command

- [ ] **Step 1: Create cmd/cost.go**

Create `cmd/cost.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Show API cost breakdown",
	RunE:  runCost,
}

var (
	costMonth   string
	costToday   bool
	costSession string
)

func init() {
	costCmd.GroupID = "human"
	rootCmd.AddCommand(costCmd)
	costCmd.Flags().StringVar(&costMonth, "month", "", "month to show (YYYY-MM format, default: current month)")
	costCmd.Flags().BoolVar(&costToday, "today", false, "show today's cost only")
	costCmd.Flags().StringVar(&costSession, "session", "", "show cost for a specific session")
}

func runCost(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	if costSession != "" {
		return runCostSession(d, costSession)
	}
	if costToday {
		return runCostToday(d)
	}
	return runCostMonth(d)
}

func runCostMonth(d interface {
	QueryTotalCost(string, string) (float64, int, int, error)
	QueryDailyCostByDate(string, string) ([]db.DateSummary, error)
	QueryDailyCostBySession(string, string) ([]db.SessionSummary, error)
}) error {
	var year, month int
	if costMonth != "" {
		t, err := time.Parse("2006-01", costMonth)
		if err != nil {
			return fmt.Errorf("invalid --month format, use YYYY-MM: %w", err)
		}
		year = t.Year()
		month = int(t.Month())
	} else {
		now := time.Now().UTC()
		year = now.Year()
		month = int(now.Month())
	}

	startDate := fmt.Sprintf("%04d-%02d-01", year, month)
	endDate := fmt.Sprintf("%04d-%02d-%02d", year, month, daysInMonth(year, time.Month(month)))

	totalCost, totalInput, totalOutput, err := d.QueryTotalCost(startDate, endDate)
	if err != nil {
		return err
	}

	days, err := d.QueryDailyCostByDate(startDate, endDate)
	if err != nil {
		return err
	}

	sessions, err := d.QueryDailyCostBySession(startDate, endDate)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"period":        fmt.Sprintf("%04d-%02d", year, month),
			"total_cost":    totalCost,
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"by_day":        days,
			"by_session":    sessions,
		})
	}

	monthName := time.Month(month).String()
	fmt.Printf("%s %d: $%.2f\n", monthName, year, totalCost)

	sessionCount := len(sessions)
	fmt.Printf("  %d sessions | %s input tokens | %s output tokens\n\n",
		sessionCount, formatTokens(totalInput), formatTokens(totalOutput))

	if len(days) > 0 {
		fmt.Println("  By day:")
		for _, day := range days {
			t, _ := time.Parse("2006-01-02", day.Date)
			fmt.Printf("    %s  $%.2f  (%d sessions)\n",
				t.Format("Jan 02"), day.CostUSD, day.SessionCount)
		}
		fmt.Println()
	}

	if len(sessions) > 0 {
		fmt.Println("  Top sessions:")
		limit := 10
		if len(sessions) < limit {
			limit = len(sessions)
		}
		for _, s := range sessions[:limit] {
			name := s.SessionName
			if name == "" {
				name = s.SessionID[:8]
			}
			fmt.Printf("    %-30s $%.2f\n", name, s.CostUSD)
		}
	}

	return nil
}

func runCostToday(d interface {
	QueryTotalCost(string, string) (float64, int, int, error)
	QueryDailyCostBySession(string, string) ([]db.SessionSummary, error)
}) error {
	today := time.Now().UTC().Format("2006-01-02")

	totalCost, totalInput, totalOutput, err := d.QueryTotalCost(today, today)
	if err != nil {
		return err
	}

	sessions, err := d.QueryDailyCostBySession(today, today)
	if err != nil {
		return err
	}

	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"date":          today,
			"total_cost":    totalCost,
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"by_session":    sessions,
		})
	}

	t, _ := time.Parse("2006-01-02", today)
	fmt.Printf("Today (%s): $%.2f\n", t.Format("Jan 02"), totalCost)

	sessionCount := len(sessions)
	fmt.Printf("  %d sessions | %s input tokens | %s output tokens\n\n",
		sessionCount, formatTokens(totalInput), formatTokens(totalOutput))

	if len(sessions) > 0 {
		fmt.Println("  By session:")
		for _, s := range sessions {
			name := s.SessionName
			if name == "" {
				name = s.SessionID[:8]
			}
			fmt.Printf("    %-30s $%.2f\n", name, s.CostUSD)
		}
	}

	return nil
}

func runCostSession(d interface {
	GetCostSnapshot(string) (*db.CostSnapshot, error)
	GetTotalAdjustment(string) (float64, error)
	GetDailyCostForSession(string, string) (*db.DailyCost, error)
	GetSession(string) (*db.Session, error)
}, sessionID string) error {
	session, err := d.GetSession(sessionID)
	if err != nil || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	snap, err := d.GetCostSnapshot(sessionID)
	if err != nil {
		return err
	}

	adjustment, err := d.GetTotalAdjustment(sessionID)
	if err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")
	todayCost, _ := d.GetDailyCostForSession(sessionID, today)

	if jsonOutput {
		result := map[string]interface{}{
			"session_id":   sessionID,
			"session_name": session.SessionName,
			"adjustment":   adjustment,
		}
		if snap != nil {
			result["reported_cost"] = snap.ReportedCostUSD
			result["true_cost"] = snap.ReportedCostUSD + adjustment
			result["model"] = snap.Model
		}
		if todayCost != nil {
			result["today_cost"] = todayCost.CostUSD
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	name := session.SessionName
	if name == "" {
		name = sessionID[:8]
	}
	fmt.Printf("Session: %s\n", name)

	if snap != nil {
		trueCost := snap.ReportedCostUSD + adjustment
		fmt.Printf("  True cost:     $%.2f\n", trueCost)
		fmt.Printf("  Reported cost: $%.2f\n", snap.ReportedCostUSD)
		if adjustment > 0 {
			fmt.Printf("  Adjustments:   $%.2f (restart resets)\n", adjustment)
		}
		fmt.Printf("  Model:         %s\n", snap.Model)
	} else {
		fmt.Println("  No cost data recorded yet")
	}

	if todayCost != nil {
		fmt.Printf("  Today:         $%.2f\n", todayCost.CostUSD)
	}

	return nil
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
```

Note: the `runCostMonth`, `runCostToday`, and `runCostSession` functions use interface parameters in the plan for readability, but in the actual implementation they should take `*db.DB` directly (the interface form is just to show what methods are called). Fix this when implementing — replace the interface parameter with `*db.DB`.

- [ ] **Step 2: Fix imports**

Ensure `cmd/cost.go` imports `"github.com/mturley/agent-handler/db"` and all references to `db.DateSummary`, `db.SessionSummary`, etc. resolve correctly.

- [ ] **Step 3: Build and verify**

Run: `go build ./...`

Expected: Compiles with no errors.

- [ ] **Step 4: Manual test**

Run: `handler cost` (may show empty data if no cost has been captured yet)
Run: `handler cost --today`
Run: `handler cost --json`

Expected: Each command produces output without errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/cost.go
git commit --signoff -m "feat: add handler cost CLI command

Show monthly, daily, and per-session cost breakdowns.
Supports --month, --today, --session, and --json flags."
```

---

### Task 4: Statusline Enhancements

Update the statusline model line to show true session cost and today's spend. Add aggregate cost line to handler session.

**Files:**
- Modify: `cmd/statusline.go` — update `formatModelLine` signature and rendering, update `renderHandlerStatusline` to add cost line

**Interfaces:**
- Consumes: `db.GetTotalAdjustment`, `db.GetDailyCostForSession`, `db.QueryTotalCost` (from Task 1)

- [ ] **Step 1: Update formatModelLine to accept cost data**

Change the `formatModelLine` function signature and body. The function currently takes `*hookInput` and reads `input.Cost.TotalCostUSD` directly. Change it to also accept the true session cost and today's session cost:

```go
func formatModelLine(input *hookInput, trueCost float64, todayCost float64) string {
	pct := input.ContextWindow.UsedPercentage
	filled := pct * 20 / 100
	empty := 20 - filled

	bar := strings.Repeat("▓", filled) + strings.Repeat("░", empty)

	barColor := colorGreen
	if pct >= 80 {
		barColor = colorRed
	} else if pct >= 50 {
		barColor = colorYellow
	}

	costStr := fmt.Sprintf("$%.2f", trueCost)
	if todayCost > 0 {
		costStr += fmt.Sprintf(" ($%.2f today)", todayCost)
	}

	return fmt.Sprintf("%s%s%s %s%s%s %d%% ctx %s| %s%s",
		colorClaudeOrange, input.Model.DisplayName, colorReset,
		barColor, bar, colorReset,
		pct,
		colorDim, costStr, colorReset)
}
```

- [ ] **Step 2: Compute cost data in the read-only rendering section**

In `runStatuslineFromHook`, after the read-only DB is opened and the session is fetched, compute the cost display values. Add this after the `isHandler` assignment (around line 122):

```go
	// Compute cost display values
	trueCost := input.Cost.TotalCostUSD
	todayCost := 0.0
	if input.Cost.TotalCostUSD > 0 {
		if adj, err := d.GetTotalAdjustment(input.SessionID); err == nil {
			trueCost += adj
		}
		today := time.Now().UTC().Format("2006-01-02")
		if dc, err := d.GetDailyCostForSession(input.SessionID, today); err == nil && dc != nil {
			todayCost = dc.CostUSD
		}
	}
```

- [ ] **Step 3: Update all formatModelLine call sites**

Find the two places `formatModelLine` is called (in `renderWorkerStatusline` and `renderHandlerStatusline`) and pass the new parameters:

In `renderWorkerStatusline`:
```go
	if input != nil && input.Model.DisplayName != "" {
		fmt.Println(formatModelLine(input, trueCost, todayCost))
	}
```

In `renderHandlerStatusline`:
```go
	if input != nil && input.Model.DisplayName != "" {
		fmt.Println(formatModelLine(input, trueCost, todayCost))
	}
```

Both functions need to receive `trueCost` and `todayCost` as parameters — update their signatures and the call sites in `runStatuslineFromHook` accordingly.

- [ ] **Step 4: Add aggregate cost line to handler statusline**

In `renderHandlerStatusline`, after the model line and before the inbox line, add:

```go
	// Aggregate cost line
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()
	monthStart := fmt.Sprintf("%04d-%02d-01", now.Year(), now.Month())
	monthEnd := fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), daysInMonth(now.Year(), now.Month()))

	todayTotal, _, _, _ := d.QueryTotalCost(today, today)
	monthTotal, _, _, _ := d.QueryTotalCost(monthStart, monthEnd)

	if todayTotal > 0 || monthTotal > 0 {
		fmt.Printf("%sCost%s: $%.2f today | $%.2f this month\n",
			colorBoldWhite, colorReset, todayTotal, monthTotal)
	}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./...`

Expected: Compiles with no errors.

- [ ] **Step 6: Run all tests**

Run: `go test ./...`

Expected: All PASS.

- [ ] **Step 7: Install and visually verify**

Run: `NONINTERACTIVE=1 make install`

Then check the statusline output in an active session. The model line should show the true cost and today's cost. The handler session should show the aggregate cost line.

- [ ] **Step 8: Commit**

```bash
git add cmd/statusline.go
git commit --signoff -m "feat: show true session cost and daily spend in statusline

Worker sessions show true cost + today's spend on the model line.
Handler session adds aggregate cost line for all sessions."
```
