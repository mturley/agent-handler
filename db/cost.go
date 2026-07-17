package db

import (
	"database/sql"
	"fmt"
)

type CostSnapshot struct {
	SessionID         string
	ReportedCostUSD   float64
	TotalInputTokens  int
	TotalOutputTokens int
	Model             string
	UpdatedAt         string
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
