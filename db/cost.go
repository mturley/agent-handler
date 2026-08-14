package db

import (
	"database/sql"
	"fmt"
)

type CostEpochState struct {
	SessionID          string
	PID                int
	LastObservedCost   float64
	LastObservedInput  int
	LastObservedOutput int
	Model              string
	UpdatedAt          string
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

// RecordCostTick records one statusline cost observation for a session using an
// epoch-anchored model. An epoch is one continuous Claude Code process run,
// identified by pid. Within an epoch total_cost_usd is monotonic; each tick's
// within-epoch increment is attributed to localDate in daily_cost. A pid change
// starts a new epoch whose baseline is the first observed cost, so cost recovered
// from the transcript on resume is never double-counted. Runs in one transaction
// so concurrent statusline ticks cannot interleave.
func (db *DB) RecordCostTick(sessionID string, pid int, model, now, localDate string, reportedCost float64, reportedInput, reportedOutput int) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var st CostEpochState
	var hasState bool
	err = tx.QueryRow(`
		SELECT session_id, pid, last_observed_cost, last_observed_input, last_observed_output, model, updated_at
		FROM cost_epoch_state WHERE session_id = ?
	`, sessionID).Scan(&st.SessionID, &st.PID, &st.LastObservedCost, &st.LastObservedInput, &st.LastObservedOutput, &st.Model, &st.UpdatedAt)
	if err == nil {
		hasState = true
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to read epoch state: %w", err)
	}

	// New epoch (first tick ever, or a pid change = process restart): the current
	// reported cost is the baseline. Attribute nothing this tick.
	if !hasState || pid != st.PID {
		if _, err := tx.Exec(`
			INSERT INTO cost_epoch_state (session_id, pid, last_observed_cost, last_observed_input, last_observed_output, model, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET
				pid = excluded.pid,
				last_observed_cost = excluded.last_observed_cost,
				last_observed_input = excluded.last_observed_input,
				last_observed_output = excluded.last_observed_output,
				model = excluded.model,
				updated_at = excluded.updated_at
		`, sessionID, pid, reportedCost, reportedInput, reportedOutput, model, now); err != nil {
			return fmt.Errorf("failed to open epoch: %w", err)
		}
		return tx.Commit()
	}

	// Same epoch: compute the within-epoch increment.
	costDelta := reportedCost - st.LastObservedCost
	inputDelta := reportedInput - st.LastObservedInput
	outputDelta := reportedOutput - st.LastObservedOutput

	// Always advance the reference to the latest observed value.
	if _, err := tx.Exec(`
		UPDATE cost_epoch_state SET
			last_observed_cost = ?, last_observed_input = ?, last_observed_output = ?, model = ?, updated_at = ?
		WHERE session_id = ?
	`, reportedCost, reportedInput, reportedOutput, model, now, sessionID); err != nil {
		return fmt.Errorf("failed to update epoch state: %w", err)
	}

	// Attribute only positive increments. A dip (recalculation noise) contributes
	// nothing; the next climb is measured from the dipped value.
	if costDelta > 0 {
		if _, err := tx.Exec(`
			INSERT INTO daily_cost (session_id, date, cost_usd, input_tokens, output_tokens)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(session_id, date) DO UPDATE SET
				cost_usd = daily_cost.cost_usd + excluded.cost_usd,
				input_tokens = daily_cost.input_tokens + excluded.input_tokens,
				output_tokens = daily_cost.output_tokens + excluded.output_tokens
		`, sessionID, localDate, costDelta, inputDelta, outputDelta); err != nil {
			return fmt.Errorf("failed to upsert daily cost: %w", err)
		}
	}

	return tx.Commit()
}

func (db *DB) GetCostEpochState(sessionID string) (*CostEpochState, error) {
	var st CostEpochState
	err := db.conn.QueryRow(`
		SELECT session_id, pid, last_observed_cost, last_observed_input, last_observed_output, model, updated_at
		FROM cost_epoch_state WHERE session_id = ?
	`, sessionID).Scan(&st.SessionID, &st.PID, &st.LastObservedCost, &st.LastObservedInput, &st.LastObservedOutput, &st.Model, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cost epoch state: %w", err)
	}
	return &st, nil
}

// SessionTotalCost returns the sum of all daily_cost entries for a session.
func (db *DB) SessionTotalCost(sessionID string) (float64, error) {
	var total sql.NullFloat64
	err := db.conn.QueryRow(`
		SELECT SUM(cost_usd) FROM daily_cost WHERE session_id = ?
	`, sessionID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to get session total cost: %w", err)
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

// QueryDailyCostForSession returns per-day cost rows for one session within an
// inclusive date range, oldest first.
func (db *DB) QueryDailyCostForSession(sessionID, startDate, endDate string) ([]DailyCost, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, date, cost_usd, input_tokens, output_tokens
		FROM daily_cost
		WHERE session_id = ? AND date >= ? AND date <= ?
		ORDER BY date ASC
	`, sessionID, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily cost for session: %w", err)
	}
	defer rows.Close()

	var results []DailyCost
	for rows.Next() {
		var dc DailyCost
		if err := rows.Scan(&dc.SessionID, &dc.Date, &dc.CostUSD, &dc.InputTokens, &dc.OutputTokens); err != nil {
			return nil, fmt.Errorf("failed to scan daily cost: %w", err)
		}
		results = append(results, dc)
	}
	return results, rows.Err()
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
