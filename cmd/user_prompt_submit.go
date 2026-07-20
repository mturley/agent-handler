package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"strings"
)

var userPromptSubmitCmd = &cobra.Command{
	Use:   "user-prompt-submit",
	Short: "Handle UserPromptSubmit hook events",
	Long:  "Reads Claude Code hook stdin JSON, registers if needed, bumps heartbeat, and outputs inbox directives.",
	RunE:  runUserPromptSubmit,
}

func init() {
	userPromptSubmitCmd.GroupID = "agent"
	rootCmd.AddCommand(userPromptSubmitCmd)
	userPromptSubmitCmd.Flags().Bool("from-hook", false, "read from stdin JSON (hook mode)")
}

type promptSubmitInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Prompt         string `json:"prompt"`
	SessionTitle   string `json:"session_title"`
}

func runUserPromptSubmit(cmd *cobra.Command, args []string) error {
	fromHook, _ := cmd.Flags().GetBool("from-hook")
	if !fromHook {
		return fmt.Errorf("--from-hook is required")
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	var input promptSubmitInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("failed to parse stdin JSON: %w", err)
	}

	if input.SessionID == "" {
		return nil
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Register if not yet registered (one-time cost on first prompt)
	session, _ := d.GetSession(input.SessionID)
	if session == nil || session.Status == "archived" {
		if input.TranscriptPath != "" {
			registerSession(d, &input)
		}
		session, _ = d.GetSession(input.SessionID)
		if session == nil {
			return nil
		}
	}

	isAutoInbox := input.Prompt == "/inbox --auto"

	// Heartbeat: bump last_active and last_prompt
	now := time.Now().UTC().Format(time.RFC3339)
	d.BumpLastActive(input.SessionID, now)
	d.BumpLastPrompt(input.SessionID, now)

	// Auto mode: catch up human cursor on real user prompts
	if !isAutoInbox && session.InboxMode == "auto" {
		// Check for auto-delivered events before advancing the cursor
		autoCount, _ := d.AutoDeliveredCount(input.SessionID)
		if autoCount > 0 {
			fmt.Printf("The user is back. %d event(s) were auto-delivered while they were away. Before responding to their prompt, briefly summarize what happened — look back through your conversation for the /inbox --auto results and your responses to them since the user's last real prompt.\n", autoCount)
		}
		d.CatchUpHumanCursor(input.SessionID)
	}

	// Sync session metadata (name, terminal)
	termType, termID, workspaceID := terminal.Detect()
	syncSessionMetadata(d, input.SessionID, input.SessionTitle, claudePID(), termType, termID, workspaceID)

	// On-submit mode: notify about unread events
	if session.InboxMode == "on-submit" {
		unreadCount, _, err := d.UnreadCountForSession(input.SessionID)
		if err == nil && unreadCount > 0 {
			fmt.Printf("You have %d new unread message(s). Invoke the /inbox skill now before responding to the user's prompt.\n", unreadCount)
		}
	}

	return nil
}

func registerSession(d *db.DB, input *promptSubmitInput) {
	cwd := input.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	branch := "unknown"
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}

	repo := "unknown"
	if out, err := exec.Command("git", "-C", cwd, "remote", "get-url", "origin").Output(); err == nil {
		r := strings.TrimSpace(string(out))
		// Extract owner/repo from git URL
		if idx := strings.Index(r, "github.com"); idx >= 0 {
			r = r[idx+len("github.com"):]
			r = strings.TrimPrefix(r, ":")
			r = strings.TrimPrefix(r, "/")
			r = strings.TrimSuffix(r, ".git")
			repo = r
		}
	}

	termType, termID, workspaceID := terminal.Detect()

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertSession(db.Session{
		SessionID:       input.SessionID,
		Harness:         "claude-code",
		Repo:            repo,
		Branch:          branch,
		SessionName:     input.SessionTitle,
		PID:             claudePID(),
		Status:          "active",
		InboxMode:       "manual",
		LastActive:      now,
		RegisteredAt:    now,
		JSONLPath:       input.TranscriptPath,
		TerminalType:    termType,
		TerminalID:      termID,
		CmuxWorkspaceID: workspaceID,
	})

	// Write PID cache
	sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
	os.MkdirAll(sessionsDir, 0755)
	discover.WritePIDCache(sessionsDir, claudePID(), input.SessionID)

	// Initialize cursor
	d.AdvanceCursor(input.SessionID, now)

	// Auto-subscribe from .worktree-resources
	resourcesPath := filepath.Join(cwd, ".worktree-resources")
	resources, err := worktree.ReadResources(resourcesPath)
	if err == nil && len(resources) > 0 {
		resCfg, _ := config.Read(config.DefaultPath())
		for _, r := range resources {
			resourceType, resourceID := worktree.ParseResourceID(r.ID)
			if resourceType == "" {
				continue
			}
			resURL := r.URL
			if resURL == "" && resCfg != nil {
				resURL = resCfg.DefaultResourceURL(resourceType, resourceID)
			}
			var urlPtr *string
			if resURL != "" {
				urlPtr = &resURL
			}
			d.SubscribeIfNew(db.Subscription{
				ID:           uuid.New().String(),
				SessionID:    input.SessionID,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				ResourceURL:  urlPtr,
				CreatedAt:    now,
			})
		}
	}

	// Spawn catch-up watcher runs for subscribed resources
	subs, _ := d.ListSubscriptions(input.SessionID, false)
	if len(subs) > 0 {
		cfg, _ := config.Read(config.DefaultPath())
		resourcesByService := make(map[string][]string)
		for _, sub := range subs {
			service := config.ResourceTypeToService(sub.ResourceType)
			if service != "" && cfg.IsServiceConfigured(service) {
				resourcesByService[service] = append(resourcesByService[service], sub.ResourceID)
			}
		}
		for service, resources := range resourcesByService {
			resourceList := strings.Join(resources, ",")
			go func(svc, rl string) {
				exec.Command("handler", "watcher", "run", svc, "--resources", rl).Run()
			}(service, resourceList)
		}
	}
}
