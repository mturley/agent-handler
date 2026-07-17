package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mturley/agent-handler/db"
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

func runCostMonth(d *db.DB) error {
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

func runCostToday(d *db.DB) error {
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

func runCostSession(d *db.DB, sessionID string) error {
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
