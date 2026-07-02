package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new Claude Code agent session",
	RunE:  runRegister,
}

var (
	regSessionID    string
	regBranch       string
	regRepo         string
	regPID          int
	regJSONLPath    string
	regTerminalType string
	regTerminalID   string
)

func init() {
	registerCmd.GroupID = "agent"
	rootCmd.AddCommand(registerCmd)
	registerCmd.Flags().StringVar(&regSessionID, "session-id", "", "session ID")
	registerCmd.Flags().StringVar(&regBranch, "branch", "", "branch name")
	registerCmd.Flags().StringVar(&regRepo, "repo", "", "repository path")
	registerCmd.Flags().IntVar(&regPID, "pid", 0, "process ID")
	registerCmd.Flags().StringVar(&regJSONLPath, "jsonl-path", "", "path to Claude JSONL file")
	registerCmd.Flags().StringVar(&regTerminalType, "terminal-type", "", "terminal backend type (cmux, tmux)")
	registerCmd.Flags().StringVar(&regTerminalID, "terminal-id", "", "terminal surface/pane ID")
	registerCmd.MarkFlagRequired("session-id")
	registerCmd.MarkFlagRequired("branch")
	registerCmd.MarkFlagRequired("repo")
	registerCmd.MarkFlagRequired("pid")
	registerCmd.MarkFlagRequired("jsonl-path")
}

func runRegister(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Discover session name from JSONL
	sessionName, err := discover.DiscoverSessionName(regJSONLPath)
	if err != nil {
		return fmt.Errorf("failed to discover session name: %w", err)
	}

	// Check if this session already exists (re-registration vs brand new)
	existingSession, _ := d.GetSession(regSessionID)
	isReregistration := existingSession != nil

	// Upsert session
	now := time.Now().UTC().Format(time.RFC3339)
	err = d.UpsertSession(db.Session{
		SessionID:    regSessionID,
		Harness:      "claude-code",
		Repo:         regRepo,
		Branch:       regBranch,
		SessionName:  sessionName,
		PID:          regPID,
		Status:       "active",
		InboxMode:    "on-submit",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    regJSONLPath,
		TerminalType: regTerminalType,
		TerminalID:   regTerminalID,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert session: %w", err)
	}

	// Write PID cache
	sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}
	if err := discover.WritePIDCache(sessionsDir, regPID, regSessionID); err != nil {
		return fmt.Errorf("failed to write PID cache: %w", err)
	}

	// Initialize cursor
	if isReregistration {
		// Re-registering an existing session — keep its cursor
	} else {
		// Brand new session — start with cursor = now
		d.AdvanceCursor(regSessionID, now)
	}

	// Auto-subscribe to resources from .worktree-resources
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get cwd: %w", err)
	}
	resourcesPath := filepath.Join(cwd, ".worktree-resources")
	resources, err := worktree.ReadResources(resourcesPath)
	if err == nil && len(resources) > 0 {
		for _, r := range resources {
			resourceType, resourceID := worktree.ParseResourceID(r.ID)
			if resourceType == "" {
				continue
			}
			urlPtr := &r.URL
			err = d.SubscribeIfNew(db.Subscription{
				ID:           uuid.New().String(),
				SessionID:    regSessionID,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				ResourceURL:  urlPtr,
				CreatedAt:    now,
			})
			if err != nil {
				return fmt.Errorf("failed to subscribe to %s: %w", r.ID, err)
			}
		}
	}

	// Spawn background catch-up watcher runs for subscribed resources
	subs, _ := d.ListSubscriptions(regSessionID, false)
	if len(subs) > 0 {
		cfg, _ := config.Read(config.DefaultPath())
		resourcesByService := make(map[string][]string)
		for _, sub := range subs {
			service := config.ResourceTypeToService(sub.ResourceType)
			if service != "" && cfg.IsServiceConfigured(service) {
				resourcesByService[service] = append(resourcesByService[service],
					sub.ResourceID)
			}
		}
		for service, resources := range resourcesByService {
			resourceList := strings.Join(resources, ",")
			go func(svc, rl string) {
				exec.Command("handler", "watcher", "run", svc, "--resources", rl).Run()
			}(service, resourceList)
		}
	}

	// Query unread count for catch-up summary
	unreadCount, breakdown, err := d.UnreadCountForSession(regSessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	// Get session to check inbox mode
	session, err := d.GetSession(regSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    regSessionID,
			"session_name":  sessionName,
			"status":        "active",
			"inbox_mode":    session.InboxMode,
			"unread_count":  unreadCount,
			"unread_breakdown": breakdown,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Registered session %s\n", regSessionID)
		if sessionName != "" {
			fmt.Printf("  Name: %s\n", sessionName)
		}
		fmt.Printf("  Status: active\n")
		fmt.Printf("  Inbox mode: %s\n", session.InboxMode)
		if unreadCount > 0 {
			fmt.Printf("  Unread: %d message(s)\n", unreadCount)
			for eventType, count := range breakdown {
				fmt.Printf("    - %s: %d\n", eventType, count)
			}
		} else {
			fmt.Println("  Unread: No new messages")
		}
		if session.InboxMode == "auto" {
			fmt.Println("\n💡 Inbox mode is 'auto' — restart polling with /inbox-mode auto")
		}

		// Emit mandate — this lands in the conversation as a system-reminder
		// and ensures every session knows to emit events without needing skill invocation.
		fmt.Println("")
		fmt.Println("IMPORTANT: You must emit events regularly with `handler emit` so the ledger")
		fmt.Println("reflects what you're doing. Invoke /using-handler (once per session) for details.")
	}

	return nil
}
