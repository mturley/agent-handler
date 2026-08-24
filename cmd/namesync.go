package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktreeinterop"
	"github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

// slackNameSyncInterval throttles the heartbeat name-sync so it runs at most
// once per window even though the statusline fires far more often.
const slackNameSyncInterval = 60 * time.Second

func slackNameSyncMarkerPath() string {
	return filepath.Join(filepath.Dir(db.DefaultPath()), "last-name-sync")
}

// slackNameSyncDue reports whether the throttle window has elapsed since the
// marker file was last touched, and touches it when it returns true. A missing
// or unreadable marker counts as due (first run).
func slackNameSyncDue() bool {
	path := slackNameSyncMarkerPath()
	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) < slackNameSyncInterval {
			return false
		}
	}
	// Touch (create or bump mtime). Best-effort: if this fails we still run,
	// which at worst means we sync every statusline until it succeeds.
	now := time.Now()
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644); err == nil {
		f.Close()
		_ = os.Chtimes(path, now, now)
	}
	return true
}

// syncSlackNames mirrors Slack-thread custom names between the worktree DB and
// this handler DB using newest-wins on updated_at. It is:
//   - best-effort: any error is swallowed so the statusline never breaks;
//   - throttled: runs at most once per slackNameSyncInterval;
//   - CLI-only toward worktree (via worktreeinterop) — it never opens the
//     worktree database; it only reads/writes this handler's own DB.
func syncSlackNames(d *db.DB, cwd string) {
	if cwd == "" || !worktreeinterop.Available() {
		return
	}
	if !slackNameSyncDue() {
		return
	}
	resources, err := worktreeinterop.ListResources(cwd)
	if err != nil {
		return
	}
	conn := d.Conn()
	for _, r := range resources {
		if r.Type != "slack" {
			continue
		}
		_ = reconcileSlackName(conn, cwd, r)
	}
}

// reconcileSlackName applies newest-wins to one Slack resource. w is the
// worktree's view; the handler's own meta is read from conn. When the two
// differ, the side with the newer updated_at wins:
//   - worktree newer  → write this handler's own DB (preserving w's timestamp);
//   - handler newer    → push to worktree via the CLI (preserving handler's ts);
//   - equal timestamps → worktree wins deterministically (avoids ping-pong).
//
// It never opens the worktree DB — the only worktree write path is
// worktreeinterop.SetName (the `worktree` binary).
func reconcileSlackName(conn *sql.DB, cwd string, w worktreeinterop.Resource) error {
	var hName, hDesc, hTs string
	if m, err := wdb.GetResourceMeta(conn, w.Type, w.ID); err != nil {
		return err
	} else if m != nil {
		hName, hDesc, hTs = m.CustomName, m.CustomDescription, m.UpdatedAt
	}

	if w.CustomName == hName && w.CustomDescription == hDesc {
		return nil // already in sync
	}

	if w.UpdatedAt >= hTs {
		// worktree newer (or tie → worktree wins): update our own DB,
		// preserving the origin timestamp so we don't look newer next pass.
		return wdb.SetResourceMetaAt(conn,
			watcher.Resource{Type: w.Type, ID: w.ID},
			w.CustomName, w.CustomDescription, w.UpdatedAt)
	}
	// handler newer: push our name to worktree via the CLI.
	return worktreeinterop.SetName(cwd,
		worktreeinterop.Resource{Type: w.Type, ID: w.ID, URL: w.URL},
		hName, hDesc, hTs)
}
