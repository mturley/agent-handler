package api

import (
	"net/http"
	"time"

	"github.com/mturley/agent-handler/config"
)

type costSummary struct {
	Enabled        bool              `json:"enabled"`
	TodayCostUSD   float64           `json:"today_cost_usd"`
	MonthCostUSD   float64           `json:"month_cost_usd"`
	AllTimeCostUSD float64           `json:"all_time_cost_usd"`
	DailyBreakdown []dailyCostEntry  `json:"daily_breakdown"`
	TopSessions    []sessionCostEntry `json:"top_sessions"`
}

type dailyCostEntry struct {
	Date         string  `json:"date"`
	CostUSD      float64 `json:"cost_usd"`
	SessionCount int     `json:"session_count"`
}

type sessionCostEntry struct {
	SessionID    string  `json:"session_id"`
	SessionName  string  `json:"session_name"`
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

func (s *Server) handleCost(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Read(config.DefaultPath())
	if cfg == nil || !cfg.ExperimentalCostDisplay() {
		writeJSON(w, http.StatusOK, costSummary{Enabled: false})
		return
	}

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	monthStart := now.Format("2006-01") + "-01"
	monthEnd := now.AddDate(0, 1, 0).Format("2006-01") + "-01"

	// Today's cost
	todayCost, _, _, _ := s.DB.QueryTotalCost(today, today)

	// This month
	monthCost, _, _, _ := s.DB.QueryTotalCost(monthStart, monthEnd)

	// All time
	allTimeCost, _, _, _ := s.DB.QueryTotalCost("2000-01-01", "2100-01-01")

	// Daily breakdown for current month
	dailyData, _ := s.DB.QueryDailyCostByDate(monthStart, monthEnd)
	daily := make([]dailyCostEntry, len(dailyData))
	for i, d := range dailyData {
		daily[i] = dailyCostEntry{
			Date:         d.Date,
			CostUSD:      d.CostUSD,
			SessionCount: d.SessionCount,
		}
	}

	// Top sessions for current month
	sessionData, _ := s.DB.QueryDailyCostBySession(monthStart, monthEnd)
	topN := 10
	if len(sessionData) < topN {
		topN = len(sessionData)
	}
	sessions := make([]sessionCostEntry, topN)
	for i := 0; i < topN; i++ {
		sessions[i] = sessionCostEntry{
			SessionID:    sessionData[i].SessionID,
			SessionName:  sessionData[i].SessionName,
			CostUSD:      sessionData[i].CostUSD,
			InputTokens:  sessionData[i].InputTokens,
			OutputTokens: sessionData[i].OutputTokens,
		}
	}

	writeJSON(w, http.StatusOK, costSummary{
		Enabled:        true,
		TodayCostUSD:   todayCost,
		MonthCostUSD:   monthCost,
		AllTimeCostUSD:  allTimeCost,
		DailyBreakdown: daily,
		TopSessions:    sessions,
	})
}
