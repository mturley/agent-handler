package cmd

import (
	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktreeinterop"
)

// autoSubscribeWorktreePrimaries subscribes the session to the worktree's
// primary resources (from `worktree resources list --json`), if the worktree
// CLI is available. Best-effort: any failure is swallowed so it never blocks
// session registration. Uses SubscribeIfNew, so user-tombstoned resources are
// not resurrected.
func autoSubscribeWorktreePrimaries(d *db.DB, sessionID, cwd, now string) {
	if !worktreeinterop.Available() {
		return
	}
	resources, err := worktreeinterop.ListPrimaryResources(cwd)
	if err != nil || len(resources) == 0 {
		return
	}
	resCfg, _ := config.Read(config.DefaultPath())
	for _, r := range resources {
		if r.Type == "" || r.ID == "" {
			continue
		}
		resURL := r.URL
		if resURL == "" && resCfg != nil {
			resURL = resCfg.DefaultResourceURL(r.Type, r.ID)
		}
		var urlPtr *string
		if resURL != "" {
			urlPtr = &resURL
		}
		_ = d.SubscribeIfNew(db.Subscription{
			ID:           uuid.New().String(),
			SessionID:    sessionID,
			ResourceType: r.Type,
			ResourceID:   r.ID,
			ResourceURL:  urlPtr,
			CreatedAt:    now,
		})
	}
}
