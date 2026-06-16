package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the current database schema (DDL statements)",
	Long:  `Print the current database schema including table and index definitions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Open database in read-only mode
		d, err := openReadOnlyDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer d.Close()

		// Query table definitions
		tableRows, err := d.Conn().Query("SELECT sql FROM sqlite_master WHERE type='table' AND sql IS NOT NULL ORDER BY name")
		if err != nil {
			return fmt.Errorf("failed to query tables: %w", err)
		}
		defer tableRows.Close()

		for tableRows.Next() {
			var ddl string
			if err := tableRows.Scan(&ddl); err != nil {
				return fmt.Errorf("failed to scan table DDL: %w", err)
			}
			fmt.Println(ddl + ";")
			fmt.Println()
		}

		if err := tableRows.Err(); err != nil {
			return err
		}

		// Query index definitions
		indexRows, err := d.Conn().Query("SELECT sql FROM sqlite_master WHERE type='index' AND sql IS NOT NULL ORDER BY name")
		if err != nil {
			return fmt.Errorf("failed to query indexes: %w", err)
		}
		defer indexRows.Close()

		for indexRows.Next() {
			var ddl string
			if err := indexRows.Scan(&ddl); err != nil {
				return fmt.Errorf("failed to scan index DDL: %w", err)
			}
			fmt.Println(ddl + ";")
		}

		return indexRows.Err()
	},
}

func init() {
	schemaCmd.GroupID = "human"
	rootCmd.AddCommand(schemaCmd)
}
