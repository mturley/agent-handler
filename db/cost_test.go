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
