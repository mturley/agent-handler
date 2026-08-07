package cmd

import (
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"
)

var uiOpenCmd = &cobra.Command{
	Use:   "ui-open [session-name-or-id]",
	Short: "Open the web dashboard focused on a session (defaults to the current session)",
	Long: "Open a browser to the handler web UI's single-session page for the given session " +
		"(by name, ID, or branch). Detects whether the dev (5173) or prod (8420) server is " +
		"running. With no argument, targets the current session.",
	SilenceUsage: true,
	RunE:         runUIOpen,
}

func init() {
	uiOpenCmd.GroupID = "human"
	rootCmd.AddCommand(uiOpenCmd)
	uiOpenCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

// uiPortForRunningServer returns the port of a listening handler UI server,
// preferring the dev server (5173) over prod (8420). Returns 0 if neither.
func uiPortForRunningServer() int {
	for _, port := range []int{5173, 8420} {
		// Dial "localhost" (not 127.0.0.1) so both IPv4 and IPv6 loopback are
		// tried — the Vite dev server binds IPv6 (::1) only.
		addr := fmt.Sprintf("localhost:%d", port)
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return port
		}
	}
	return 0
}

func runUIOpen(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Resolve target: positional arg (name/id/branch), or the current session.
	// With no arg and no determinable current session, fall back to the main
	// page rather than aborting.
	var sessionID string
	if len(args) > 0 && args[0] != "" {
		session, err := resolveSessionByTarget(d, args[0])
		if err != nil {
			return err
		}
		sessionID = session.SessionID
	} else {
		// Ignore the error — an empty sessionID means "open the main page".
		sessionID, _ = resolveSessionID(cmd)
	}

	port := uiPortForRunningServer()
	if port == 0 {
		return fmt.Errorf("no handler UI server is running. Start one with `handler ui` in another terminal (or `make dev` for development)")
	}

	base := fmt.Sprintf("http://localhost:%d", port)
	url := base + "/"
	if sessionID != "" {
		url = fmt.Sprintf("%s/sessions/%s", base, sessionID)
	}
	openBrowser(url)
	fmt.Printf("Opening %s\n", url)
	return nil
}
