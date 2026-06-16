package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query SQL",
	Short: "Execute a read-only SQL query against the agent-handler database",
	Long: `Execute a read-only SQL query against the agent-handler database.

Only SELECT queries are allowed. Write operations (INSERT, UPDATE, DELETE, etc.) are rejected.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sqlQuery := args[0]

		// Safety check: reject write operations
		normalizedSQL := strings.TrimSpace(strings.ToUpper(sqlQuery))
		writeOps := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "ATTACH"}
		for _, op := range writeOps {
			if strings.HasPrefix(normalizedSQL, op) {
				return fmt.Errorf("write operations are not allowed in query command")
			}
		}

		// Open database in read-only mode
		d, err := openReadOnlyDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer d.Close()

		// Execute the query
		rows, err := d.Conn().Query(sqlQuery)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()

		// Get column names
		columns, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("failed to get columns: %w", err)
		}

		if jsonOutput {
			return outputJSON(rows, columns)
		}
		return outputText(rows, columns)
	},
}

func outputText(rows *sql.Rows, columns []string) error {
	// Print column headers
	for i, col := range columns {
		if i > 0 {
			fmt.Print("\t")
		}
		fmt.Print(col)
	}
	fmt.Println()

	// Print rows
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		for i, val := range values {
			if i > 0 {
				fmt.Print("\t")
			}
			if val == nil {
				fmt.Print("NULL")
			} else {
				fmt.Print(val)
			}
		}
		fmt.Println()
	}

	return rows.Err()
}

func outputJSON(rows *sql.Rows, columns []string) error {
	var results []map[string]interface{}

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func init() {
	queryCmd.GroupID = "human"
	rootCmd.AddCommand(queryCmd)
}
