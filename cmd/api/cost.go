package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/mturley/agent-handler/config"
)

type costSummary struct {
	Enabled        bool            `json:"enabled"`
	TodayCostUSD   float64         `json:"today_cost_usd"`
	AllTimeCostUSD float64         `json:"all_time_cost_usd"`
	Months         []monthSummary  `json:"months"`
}

type monthSummary struct {
	Label          string             `json:"label"`
	CostUSD        float64            `json:"cost_usd"`
	DailyBreakdown []dailyCostEntry   `json:"daily_breakdown"`
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

	todayCost, _, _, _ := s.DB.QueryTotalCost(today, today)
	allTimeCost, _, _, _ := s.DB.QueryTotalCost("2000-01-01", "2100-01-01")

	months := []monthSummary{
		s.buildMonthSummary(now),
		s.buildMonthSummary(now.AddDate(0, -1, 0)),
	}

	writeJSON(w, http.StatusOK, costSummary{
		Enabled:        true,
		TodayCostUSD:   todayCost,
		AllTimeCostUSD: allTimeCost,
		Months:         months,
	})
}

func (s *Server) buildMonthSummary(t time.Time) monthSummary {
	label := t.Format("January 2006")
	monthStart := t.Format("2006-01") + "-01"
	nextMonth := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := nextMonth.Format("2006-01-02")

	monthCost, _, _, _ := s.DB.QueryTotalCost(monthStart, monthEnd)

	dailyData, _ := s.DB.QueryDailyCostByDate(monthStart, monthEnd)
	sort.Slice(dailyData, func(i, j int) bool {
		return dailyData[i].Date < dailyData[j].Date
	})
	daily := make([]dailyCostEntry, len(dailyData))
	for i, d := range dailyData {
		daily[i] = dailyCostEntry{
			Date:         d.Date,
			CostUSD:      d.CostUSD,
			SessionCount: d.SessionCount,
		}
	}

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

	return monthSummary{
		Label:          label,
		CostUSD:        monthCost,
		DailyBreakdown: daily,
		TopSessions:    sessions,
	}
}
