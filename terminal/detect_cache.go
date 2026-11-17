package terminal

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/internal/fscache"
)

// detectFn and now are package-level so tests can swap in fakes.
var (
	detectFn = Detect
	now      = time.Now
)

// detectTTL is how long a cached detection result is trusted before DetectCached
// calls detectFn again. 5m rather than the statusline's 60s refresh interval,
// so most refreshes hit the cache.
const detectTTL = 5 * time.Minute

// detectCacheEntry is the on-disk shape of a cached DetectCached result.
type detectCacheEntry struct {
	Surface   string
	Tmux      string
	WrittenAt time.Time
	Type      string
	ID        string
	Workspace string
}

func detectCacheKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])[:16]
}

// DetectCached is Detect backed by a per-session on-disk cache in cacheDir,
// keyed by sessionID plus the current CMUX_SURFACE_ID and TMUX env values. A
// warm hit within detectTTL avoids spawning `cmux identify` / `tmux
// display-message`. A result with an empty terminal id, or an empty
// workspace id under cmux, is never cached, so a failed detection is retried
// on the next call rather than sticking until the TTL expires.
//
// An empty sessionID calls detectFn uncached.
func DetectCached(cacheDir, sessionID string) (backendType, terminalID, workspaceID string) {
	if sessionID == "" {
		return detectFn()
	}

	surface := os.Getenv("CMUX_SURFACE_ID")
	tmux := os.Getenv("TMUX")
	path := filepath.Join(cacheDir, detectCacheKey(sessionID)+".json")

	var e detectCacheEntry
	if fscache.ReadJSON(path, &e) && e.Surface == surface && e.Tmux == tmux {
		if age := now().Sub(e.WrittenAt); age >= 0 && age < detectTTL {
			return e.Type, e.ID, e.Workspace
		}
	}

	backendType, terminalID, workspaceID = detectFn()

	if terminalID == "" || (backendType == "cmux" && workspaceID == "") {
		return backendType, terminalID, workspaceID
	}

	entry := detectCacheEntry{
		Surface:   surface,
		Tmux:      tmux,
		WrittenAt: now(),
		Type:      backendType,
		ID:        terminalID,
		Workspace: workspaceID,
	}
	_ = fscache.WriteJSONAtomic(path, entry)
	fscache.Prune(cacheDir, 7*24*time.Hour, now())
	return backendType, terminalID, workspaceID
}
