package git

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mturley/agent-handler/internal/fscache"
)

// now is the package clock; tests swap it.
var now = time.Now

const (
	statusTTL     = 30 * time.Second
	baseTTL       = 10 * time.Minute
	cachePruneAge = 7 * 24 * time.Hour
)

// fingerprint captures the cheap-to-stat files whose change invalidates a
// cached Status. Mtimes are UnixNano; a missing file is 0.
type fingerprint struct {
	HeadMtime, IndexMtime, RefMtime, PackedMtime int64
	HeadSize, IndexSize                          int64
	Rebasing                                     bool
}

type statusEntry struct {
	Fingerprint fingerprint
	WrittenAt   time.Time
	Status      Status
}

type baseEntry struct {
	HeadSHA    string
	BaseRef    string
	BaseRefSHA string
	WrittenAt  time.Time
	Base       baseInfo
}

func statMtimeSize(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.ModTime().UnixNano(), info.Size()
}

// takeFingerprint stats the files guarding a cached Status and parses
// <gitdir>/HEAD, returning the branch ref ("" if detached) and HEAD sha
// ("" if unreadable). It spawns nothing.
func takeFingerprint(dirs GitDirs) (fingerprint, string, string) {
	var fp fingerprint
	fp.HeadMtime, fp.HeadSize = statMtimeSize(filepath.Join(dirs.GitDir, "HEAD"))
	fp.IndexMtime, fp.IndexSize = statMtimeSize(filepath.Join(dirs.GitDir, "index"))
	fp.PackedMtime, _ = statMtimeSize(filepath.Join(dirs.CommonDir, "packed-refs"))
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(dirs.GitDir, d)); err == nil {
			fp.Rebasing = true
		}
	}

	data, err := os.ReadFile(filepath.Join(dirs.GitDir, "HEAD"))
	if err != nil {
		return fp, "", ""
	}
	head := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(head, "ref: "); ok {
		ref = strings.TrimSpace(ref)
		fp.RefMtime, _ = statMtimeSize(filepath.Join(dirs.CommonDir, filepath.FromSlash(ref)))
		sha, _ := readRefSHA(dirs, ref)
		return fp, ref, sha
	}
	return fp, "", head
}

// readRefSHA resolves a full ref name to its sha from the loose ref file
// under CommonDir, else from CommonDir/packed-refs. It spawns nothing.
func readRefSHA(dirs GitDirs, ref string) (string, bool) {
	if data, err := os.ReadFile(filepath.Join(dirs.CommonDir, filepath.FromSlash(ref))); err == nil {
		sha := strings.TrimSpace(string(data))
		if sha != "" && !strings.HasPrefix(sha, "ref:") {
			return sha, true
		}
		return "", false
	}
	f, err := os.Open(filepath.Join(dirs.CommonDir, "packed-refs"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		sha, name, ok := strings.Cut(line, " ")
		if ok && name == ref {
			return sha, true
		}
	}
	return "", false
}

// baseRefFullName maps the base ref chosen by computeBase to a full ref name:
// "origin/main" / "upstream/main" are remote-tracking refs; a bare default
// branch is a local branch.
func baseRefFullName(baseRef string) string {
	if strings.Contains(baseRef, "/") {
		return "refs/remotes/" + baseRef
	}
	return "refs/heads/" + baseRef
}

func cacheKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// GetStatusCached is GetStatus backed by an on-disk cache in cacheDir: a
// per-cwd status entry guarded by a filesystem fingerprint and a 30s TTL, and
// a per-(commondir, branch) base entry guarded by HEAD sha, base ref sha and a
// 10m TTL. A warm hit spawns no git processes. Cache errors are misses.
func GetStatusCached(cwd, cacheDir string) *Status {
	dirs, ok := ResolveGitDir(cwd)
	if !ok {
		return &Status{DefaultBranch: "main"}
	}
	// Fingerprint BEFORE any git command runs, so a change landing mid-compute
	// mismatches on the next call instead of being cached as current.
	fp, _, _ := takeFingerprint(dirs)

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	statusPath := filepath.Join(cacheDir, "status-"+cacheKey(absCwd)+".json")

	var e statusEntry
	if fscache.ReadJSON(statusPath, &e) && e.Fingerprint == fp {
		if age := now().Sub(e.WrittenAt); age >= 0 && age < statusTTL {
			s := e.Status
			return &s
		}
	}

	s := computeStatus(cwd, dirs, func(branch string) baseInfo {
		return cachedBase(cwd, dirs, branch, cacheDir)
	})
	_ = fscache.WriteJSONAtomic(statusPath, statusEntry{Fingerprint: fp, WrittenAt: now(), Status: *s})
	fscache.Prune(cacheDir, cachePruneAge, now())
	return s
}

// cachedBase returns base info for branch from the base entry when it is
// still valid, else computes it with computeBase and overwrites the entry.
func cachedBase(cwd string, dirs GitDirs, branch, cacheDir string) baseInfo {
	// HEAD sha read now (just after the status command), before computeBase
	// runs: if HEAD moves mid-compute the stored sha is the older one and the
	// next lookup misses, rather than caching new results as current.
	_, _, headSHA := takeFingerprint(dirs)
	path := filepath.Join(cacheDir, "base-"+cacheKey(dirs.CommonDir+"\x00"+branch)+".json")

	var e baseEntry
	if headSHA != "" && fscache.ReadJSON(path, &e) && e.HeadSHA == headSHA {
		age := now().Sub(e.WrittenAt)
		if age >= 0 && age < baseTTL && validBaseRef(dirs, e) {
			return e.Base
		}
	}

	b := computeBase(cwd, dirs, branch)
	if headSHA == "" {
		return b
	}
	entry := baseEntry{HeadSHA: headSHA, BaseRef: b.BaseRef, WrittenAt: now(), Base: b}
	if b.BaseRef != "" {
		entry.BaseRefSHA, _ = readRefSHA(dirs, baseRefFullName(b.BaseRef))
	}
	_ = fscache.WriteJSONAtomic(path, entry)
	return b
}

// validBaseRef reports whether the entry's base ref still points where it did.
// Entries with no base ref (branch is the default branch) have nothing to
// compare. An unreadable base ref sha is a miss.
func validBaseRef(dirs GitDirs, e baseEntry) bool {
	if e.BaseRef == "" {
		return true
	}
	sha, ok := readRefSHA(dirs, baseRefFullName(e.BaseRef))
	return ok && sha == e.BaseRefSHA
}
