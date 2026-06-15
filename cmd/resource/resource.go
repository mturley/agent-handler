package resource

import "github.com/spf13/cobra"

// JSONOutput is set by the parent cmd package to enable JSON output
var JSONOutput *bool

var ResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Resource relationship management",
}
