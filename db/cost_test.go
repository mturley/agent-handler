package db

import (
	"testing"
)

func TestGetCostEpochStateNotFound(t *testing.T) {
	d := testDB(t)
	st, err := d.GetCostEpochState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Error("expected nil for nonexistent session")
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

func TestRecordCostTickFirstTickNoAttribution(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-first")

	// First tick ever establishes the baseline and attributes nothing.
	if err := d.RecordCostTick("epoch-first", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 5.00, 50000, 2000); err != nil {
		t.Fatalf("RecordCostTick failed: %v", err)
	}

	dc, _ := d.GetDailyCostForSession("epoch-first", "2026-08-14")
	if dc != nil {
		t.Errorf("expected no daily cost on first tick, got %v", dc)
	}
	st, _ := d.GetCostEpochState("epoch-first")
	if st == nil || st.PID != 100 || st.LastObservedCost != 5.00 {
		t.Fatalf("expected epoch state pid=100 cost=5.00, got %v", st)
	}
}

func TestRecordCostTickSamePIDIncrease(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-inc")

	d.RecordCostTick("epoch-inc", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 5.00, 50000, 2000)
	// Same PID, cost climbs — increment attributed to the local date.
	d.RecordCostTick("epoch-inc", 100, "opus", "2026-08-14T10:00:10Z", "2026-08-14", 9.00, 90000, 3000)

	dc, _ := d.GetDailyCostForSession("epoch-inc", "2026-08-14")
	if dc == nil || dc.CostUSD != 4.00 {
		t.Fatalf("expected daily cost 4.00 (9-5), got %v", dc)
	}
	if dc.InputTokens != 40000 || dc.OutputTokens != 1000 {
		t.Errorf("expected token deltas 40000/1000, got %d/%d", dc.InputTokens, dc.OutputTokens)
	}
}

func TestRecordCostTickSamePIDUnchanged(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-same")

	d.RecordCostTick("epoch-same", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 5.00, 50000, 2000)
	d.RecordCostTick("epoch-same", 100, "opus", "2026-08-14T10:00:10Z", "2026-08-14", 9.00, 90000, 3000)
	// No change on this tick.
	d.RecordCostTick("epoch-same", 100, "opus", "2026-08-14T10:00:20Z", "2026-08-14", 9.00, 90000, 3000)

	dc, _ := d.GetDailyCostForSession("epoch-same", "2026-08-14")
	if dc.CostUSD != 4.00 {
		t.Errorf("expected daily cost 4.00 (unchanged), got %f", dc.CostUSD)
	}
}

func TestRecordCostTickSamePIDDip(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-dip")

	d.RecordCostTick("epoch-dip", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 20.00, 200000, 10000)
	// A downward dip within the SAME epoch (recalc noise) attributes nothing.
	d.RecordCostTick("epoch-dip", 100, "opus", "2026-08-14T10:00:10Z", "2026-08-14", 12.00, 120000, 6000)

	dc, _ := d.GetDailyCostForSession("epoch-dip", "2026-08-14")
	if dc != nil {
		t.Fatalf("expected no daily cost row after baseline+dip, got %v", dc)
	}
	// Reference tracks the dipped value; next climb is measured from it.
	st, _ := d.GetCostEpochState("epoch-dip")
	if st.LastObservedCost != 12.00 {
		t.Errorf("expected reference 12.00 after dip, got %f", st.LastObservedCost)
	}
	d.RecordCostTick("epoch-dip", 100, "opus", "2026-08-14T10:00:20Z", "2026-08-14", 15.00, 150000, 7000)
	dc2, _ := d.GetDailyCostForSession("epoch-dip", "2026-08-14")
	if dc2.CostUSD != 3.00 {
		t.Errorf("expected daily cost 3.00 (15-12), got %f", dc2.CostUSD)
	}
}

func TestRecordCostTickRestartFreshGauge(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-restart")

	d.RecordCostTick("epoch-restart", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 5.00, 50000, 2000)
	d.RecordCostTick("epoch-restart", 100, "opus", "2026-08-14T10:00:10Z", "2026-08-14", 20.00, 200000, 8000)
	// daily so far: 15.00

	// Restart: new PID, gauge starts fresh at ~0. First tick of new epoch = baseline.
	d.RecordCostTick("epoch-restart", 200, "opus", "2026-08-14T12:00:00Z", "2026-08-14", 0.50, 5000, 100)
	// Climb within the new epoch.
	d.RecordCostTick("epoch-restart", 200, "opus", "2026-08-14T12:00:10Z", "2026-08-14", 6.50, 60000, 1100)

	total, _ := d.SessionTotalCost("epoch-restart")
	if total != 21.00 {
		t.Errorf("expected total 21.00 (15 + 6 post-restart), got %f", total)
	}
	st, _ := d.GetCostEpochState("epoch-restart")
	if st.PID != 200 {
		t.Errorf("expected pid 200 after restart, got %d", st.PID)
	}
}

func TestRecordCostTickRestartRecoveredCost(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-recover")

	d.RecordCostTick("epoch-recover", 100, "opus", "2026-08-14T10:00:00Z", "2026-08-14", 5.00, 50000, 2000)
	d.RecordCostTick("epoch-recover", 100, "opus", "2026-08-14T10:00:10Z", "2026-08-14", 20.00, 200000, 8000)
	// daily so far: 15.00

	// Restart: new PID, but Claude recovers cost from transcript — gauge starts
	// at the OLD peak (~20) instead of ~0. Baseline = 20, so no double-count.
	d.RecordCostTick("epoch-recover", 200, "opus", "2026-08-14T12:00:00Z", "2026-08-14", 20.00, 200000, 8000)
	// New work climbs to 23.
	d.RecordCostTick("epoch-recover", 200, "opus", "2026-08-14T12:00:10Z", "2026-08-14", 23.00, 230000, 9000)

	total, _ := d.SessionTotalCost("epoch-recover")
	if total != 18.00 {
		t.Errorf("expected total 18.00 (15 + 3 new, no double-count), got %f", total)
	}
}

func TestRecordCostTickLocalDateAttribution(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "epoch-dates")

	d.RecordCostTick("epoch-dates", 100, "opus", "2026-08-14T23:59:00Z", "2026-08-14", 5.00, 50000, 2000)
	d.RecordCostTick("epoch-dates", 100, "opus", "2026-08-14T23:59:10Z", "2026-08-14", 8.00, 80000, 2500)
	// Next tick observed on the following local day.
	d.RecordCostTick("epoch-dates", 100, "opus", "2026-08-15T00:00:10Z", "2026-08-15", 12.00, 120000, 3000)

	d14, _ := d.GetDailyCostForSession("epoch-dates", "2026-08-14")
	d15, _ := d.GetDailyCostForSession("epoch-dates", "2026-08-15")
	if d14.CostUSD != 3.00 {
		t.Errorf("expected 3.00 on 08-14, got %f", d14.CostUSD)
	}
	if d15.CostUSD != 4.00 {
		t.Errorf("expected 4.00 on 08-15, got %f", d15.CostUSD)
	}
	total, _ := d.SessionTotalCost("epoch-dates")
	if total != 7.00 {
		t.Errorf("expected total 7.00, got %f", total)
	}
}
