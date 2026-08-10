package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mturley/agent-handler/cmd/resource"
	"github.com/mturley/agent-handler/cmd/watcher"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var globalWebFS embed.FS

func SetWebFS(fs embed.FS) {
	globalWebFS = fs
}

var rootCmd = &cobra.Command{
	Use:   "handler",
	Short: "Agent handler CLI for managing Claude Code agent sessions",
	Long:  `A CLI tool backed by SQLite for managing Claude Code agent sessions.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Commands that must run before setup / without a database at all.
		// (`ui` is intentionally NOT here: it needs a set-up, migrated DB.)
		if cmd.Name() == "setup" || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "claude" {
			return nil
		}
		dbPath := db.DefaultPath()
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "agent-handler is not set up yet. Run 'handler setup' to configure skills, hooks, and database.")
			os.Exit(1)
		}
		// Refuse to run against an unmigrated legacy database — the schema
		// changed when handler adopted the watcher library, and commands
		// would read/write the wrong tables until the data is migrated.
		if commandGuardedForLegacyDB(cmd.CommandPath()) && legacyUnmigrated() {
			fmt.Fprintln(os.Stderr, legacyMigrationRequiredMessage)
			os.Exit(1)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	rootCmd.AddGroup(
		&cobra.Group{ID: "human", Title: "Commands for humans:"},
		&cobra.Group{ID: "agent", Title: "Commands for agents (used by hooks and skills):"},
		&cobra.Group{ID: "admin", Title: "Admin:"},
	)

	// Set up resource subcommand
	resource.JSONOutput = &jsonOutput
	resource.ResourceCmd.GroupID = "human"
	rootCmd.AddCommand(resource.ResourceCmd)

	// Set up watcher subcommand
	watcher.JSONOutput = &jsonOutput
	watcher.WatcherCmd.GroupID = "human"
	rootCmd.AddCommand(watcher.WatcherCmd)
}

// legacyMigrationRequiredMessage is shown when a handler command is run against
// a database that still holds unmigrated pre-watcher-library data. It reuses the
// same wording as the `handler setup` legacy guard (legacyDBError).
const legacyMigrationRequiredMessage = legacyDBError

// legacyGuardExemptCommands are commands (by full cobra command path) that must
// run even against an unmigrated legacy database:
//   - handler setup: runs the migration (and has its own legacy guard for plain
//     setup).
//   - handler help/completion: must always work.
//   - handler claude: launches a Claude session; it does not itself read the
//     legacy watcher tables (registration happens later via the statusline hook,
//     which is exempt and shows the migration warning).
//   - handler statusline: surfaces the migration warning itself; erroring here
//     would break every session's prompt.
//   - handler uninstall / handler watcher uninstall: must be usable to tear down
//     a broken install; neither touches the legacy data.
//
// NOTE: `handler ui` is deliberately NOT exempt — the web dashboard's API reads
// the legacy subscriptions/resource_state tables directly, so it must refuse
// (and force a migration) rather than silently serve stale data.
//
// Matching is by full command path (cmd.CommandPath()), not leaf name, so a
// future subcommand that happens to reuse a leaf name (e.g. another `status`)
// is not accidentally exempted.
var legacyGuardExemptCommands = map[string]bool{
	"handler setup":             true,
	"handler help":              true,
	"handler completion":        true,
	"handler claude":            true,
	"handler statusline":        true,
	"handler uninstall":         true,
	"handler watcher uninstall": true,
}

// commandGuardedForLegacyDB reports whether a command with the given full cobra
// command path should be blocked when the database holds unmigrated legacy data.
func commandGuardedForLegacyDB(commandPath string) bool {
	return !legacyGuardExemptCommands[commandPath]
}

// legacyUnmigrated reports whether the real handler database holds unmigrated
// legacy watcher data. It is best-effort: if the database can't be opened, it
// returns false so the command proceeds (and surfaces the real open error
// itself) rather than being blocked by a transient read failure.
func legacyUnmigrated() bool {
	// Read-only open: HasUnmigratedLegacyData only issues SELECTs, and the
	// caller has already confirmed the DB file exists. Avoids a second full
	// schema/migration pass (db.Open runs DDL + migrations) on every command,
	// including the high-frequency statusline hook.
	d, err := db.OpenReadOnly(db.DefaultPath())
	if err != nil {
		return false
	}
	defer d.Close()
	return d.HasUnmigratedLegacyData()
}

func openDB() (*db.DB, error) {
	return db.Open(db.DefaultPath())
}

func openReadOnlyDB() (*db.DB, error) {
	return db.OpenReadOnly(db.DefaultPath())
}

func resolveSessionID(cmd *cobra.Command) (string, error) {
	sessionID, _ := cmd.Flags().GetString("session-id")
	if sessionID != "" {
		return sessionID, nil
	}
	return discover.ResolveSessionID(db.HandlerHome())
}

// resolveSessionByTarget finds a session by UUID, name, or branch.
func resolveSessionByTarget(d *db.DB, target string) (*db.Session, error) {
	// Try exact session ID match first
	session, err := d.GetSession(target)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session != nil {
		return session, nil
	}

	// Try session name match
	sessions, err := d.ListSessions(false, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var matches []*db.Session
	for i := range sessions {
		s := &sessions[i]
		if s.SessionName == target || s.Branch == target {
			matches = append(matches, s)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple sessions match %q — use full session ID", target)
	}

	return nil, fmt.Errorf("session %q not found", target)
}

// claudePID returns the Claude process PID. Hooks set CLAUDE_PID=$PPID before
// invoking the Go binary, because the Go binary is a grandchild of Claude
// (Claude → bash → Go) and os.Getppid() would return the bash PID.
func claudePID() int {
	if pidStr := os.Getenv("CLAUDE_PID"); pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			return pid
		}
	}
	return os.Getppid()
}

// findSessionsWithUnreads returns sessions (other than self) that have human-unread events.
func findSessionsWithUnreads(d *db.DB, selfSessionID string) []db.Session {
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return nil
	}

	var withUnreads []db.Session
	for _, s := range sessions {
		if s.SessionID == selfSessionID || s.Role == "handler" {
			continue
		}
		if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
			continue
		}
		count, err := d.HumanUnreadCountForSession(s.SessionID)
		if err == nil && count > 0 {
			withUnreads = append(withUnreads, s)
		}
	}
	return withUnreads
}

// syncSessionMetadata updates session name, PID, and terminal info only if changed.
func syncSessionMetadata(d *db.DB, sessionID, name string, pid int, termType, termID, workspaceID, cwd, model string, contextPercent int) {
	session, err := d.GetSession(sessionID)
	if err != nil || session == nil {
		return
	}

	updates := map[string]interface{}{}
	if name != "" && session.SessionName != name {
		updates["session_name"] = name
	}
	if pid > 0 && session.PID != pid {
		updates["pid"] = pid
		sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
		discover.WritePIDCache(sessionsDir, pid, sessionID)
	}
	if termType != "" && session.TerminalType != termType {
		updates["terminal_type"] = termType
	} else if termType == "" && session.TerminalType != "" {
		// Surface no longer exists (stale after cmux restart) — clear terminal info
		updates["terminal_type"] = ""
		updates["terminal_id"] = ""
		updates["cmux_workspace_id"] = ""
		updates["cmux_workspace_name"] = ""
		updates["cmux_workspace_color"] = ""
	}
	if termID != "" && session.TerminalID != termID {
		updates["terminal_id"] = termID
	}
	if workspaceID != "" && session.CmuxWorkspaceID != workspaceID {
		updates["cmux_workspace_id"] = workspaceID
	}
	if termType == "cmux" && termID != "" {
		wsName, wsColor := terminal.CmuxWorkspaceInfo(termID)
		if wsName != "" && session.CmuxWorkspaceName != wsName {
			updates["cmux_workspace_name"] = wsName
		}
		if wsColor != "" && session.CmuxWorkspaceColor != wsColor {
			updates["cmux_workspace_color"] = wsColor
		}
	}

	if cwd != "" && session.CWD != cwd {
		updates["cwd"] = cwd
	}
	if model != "" && session.Model != model {
		updates["model"] = model
	}
	if contextPercent >= 0 && session.ContextPercent != contextPercent {
		updates["context_percent"] = contextPercent
	}

	if len(updates) == 0 {
		return
	}

	setClauses := ""
	args := []interface{}{}
	for col, val := range updates {
		if setClauses != "" {
			setClauses += ", "
		}
		setClauses += col + " = ?"
		args = append(args, val)
	}
	args = append(args, sessionID)
	d.Conn().Exec("UPDATE sessions SET "+setClauses+" WHERE session_id = ?", args...)
}
