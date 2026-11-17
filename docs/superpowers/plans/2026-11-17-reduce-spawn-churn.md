# Spawn-Churn Reduction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut the number of child processes agent-handler spawns per statusline refresh / hook call (currently ~15 per statusline, more for handler-role sessions) to near zero on the warm path, without changing any rendered output.

**Architecture:** Put all git calls in `git/` behind an injectable runner, resolve the gitdir from the filesystem, and add a per-cwd on-disk cache (fingerprinted by gitdir file mtimes plus a 30s TTL) and a base cache, one file per branch (default branch, merge-base, ahead/behind, committed stats; valid while HEAD and the base ref are unchanged, 10m TTL). Cache `terminal.Detect()` per session. Replace `kill`/`ps` spawns in `discover/` with syscalls. Slim the two shell wrappers that run most often.

**Tech Stack:** Go 1.26 (`go test ./...`), `golang.org/x/sys/unix` (already in go.mod as indirect), bash hooks embedded via `embedded.go`.

**Spec:** The task brief this plan was written from (spawn churn on a CrowdStrike Mac; statusline is the #1 source). Code facts verified on 2026-11-16 are listed under "Verified findings" below.

## Verified findings (from reading the code)

- `cmd/statusline.go:237` calls `gitpkg.GetStatus(input.CWD)` every refresh for non-handler sessions. `git/status.go:52-136` spawns: `rev-parse --git-dir` (twice, lines 56 and 93), `rev-parse --abbrev-ref HEAD`, `symbolic-ref refs/remotes/origin/HEAD`, `status --porcelain --no-lock-index`, `diff HEAD --shortstat`, up to 2x `rev-parse --verify`, `merge-base`, 2x `rev-list --count`, `diff --shortstat` = up to 12 git processes.
- **Ahead/Behind shown in the statusline are relative to the default branch's merge-base, not the upstream.** `status --porcelain=v2 --branch`'s `# branch.ab` is therefore NOT a substitute for them; v2 is used only for branch name, HEAD oid, and dirty counts. Uncommitted line counts still need `diff HEAD --shortstat`.
- `terminal.Detect()` (`terminal/backend.go:20`) runs `cmux identify --surface` (`terminal/cmux.go:98`) or `tmux display-message`. Called every refresh at `cmd/statusline.go:178` and every prompt at `cmd/user_prompt_submit.go:100` (also at registration: `cmd/statusline.go:1180,1235`, `cmd/user_prompt_submit.go:151` — leave those uncached).
- `discover.IsProcessAlive` (`discover/pid.go:61`) spawns the `kill` binary; `IsSessionProcess` (`discover/pid.go:73`) then spawns `ps -o command=`. The command line IS required (it checks for `claude` and the session id), so `ps` is replaced with a sysctl/procfs read, not removed. Called per session at `cmd/statusline.go:397` (handler role) and `:1136`, plus `cmd/status.go`, `cmd/triage.go`, `cmd/root.go`, `cmd/peek_cache.go`, `cmd/api/*`.
- `hooks/post_tool_use_wake.sh` runs on every tool call and spawns `jq` (plus `cat`). `hooks/statusline.sh` spawns `cat`.
- `cmd/user_prompt_submit.go:134,139` git calls run only inside `registerSession` (first prompt / re-registration), so they are NOT per-prompt churn. Out of scope.
- Hooks are configured by `configureHooks()` in `cmd/setup.go:~300` by script filename; scripts are embedded from `hooks/*.sh`. No settings change is needed if script names stay the same.

## Global Constraints

- Rendered statusline output must be byte-identical to today's for the same repo state (`formatGitLine` in `cmd/statusline.go:556` must not change).
- All cache files live under `filepath.Join(db.HandlerHome(), "state", ...)` (same root as `state/wake-armed` in `cmd/wake.go:207`). The `git` and `terminal` packages must not import `db`; callers pass the cache dir in.
- Cache writes are atomic: write to `os.CreateTemp(dir, ".tmp-*")` then `os.Rename`. Any cache read/parse error is treated as a miss, never surfaced.
- Status cache: invalidated by mtime/size change of `<gitdir>/HEAD`, `<gitdir>/index`, the current branch ref file (`<commondir>/refs/heads/<branch>`) or `<commondir>/packed-refs`, or presence of `<gitdir>/rebase-merge|rebase-apply`; max TTL **30s**.
- Base cache (default branch, base ref, merge-base, ahead, behind, committed adds/dels): ONE file per (commondir, branch) — never per commit, so the cache dir cannot grow per commit. The entry stores `HeadSHA`, `BaseRef` and `BaseRefSHA`; it is a hit iff the stored `HeadSHA` equals the current HEAD sha, the base ref's current sha (read from the loose ref file or `packed-refs`, no spawn) equals `BaseRefSHA`, and age < **10m**. If the base ref's sha cannot be read from files (reftable repo, unknown layout) it is a miss. This makes "behind" update on the next status miss after a fetch instead of lagging 10 minutes.
- Terminal detection cache: keyed by session id + `CMUX_SURFACE_ID` + `TMUX` env values; TTL **5m** (the statusline timer is 60s, so a 60s TTL would almost never hit). A result with an empty terminal id, or an empty workspace id under cmux, is never cached.
- Every fingerprint is taken BEFORE the git commands it guards run, and the entry is stored with that pre-compute fingerprint. A change that lands mid-compute then mismatches on the next call instead of being cached as current.
- Cache dirs are pruned: on a cache write, if `<cacheDir>/.pruned` is missing or older than 24h, delete entries not modified in 7 days and touch `.pruned`.
- Shared cache file helpers live in a new package `internal/fscache` (`WriteJSONAtomic`, `ReadJSON`, `Prune`), used by both `git` and `terminal`. No copies.
- Tests in `git/` and `terminal/` swap package-level vars (`runGit`, `now`, `detectFn`) and so must NOT call `t.Parallel()`.
- Implementers must NOT run `make install` or `handler setup`: `install` replaces the binary every live session uses and its `stop-running` target kills running `handler ui|watcher|tail` servers. Installing is a user-run step after merge (see Deployment).
- No new third-party modules; `golang.org/x/sys` moves from indirect to direct.
- Hook script filenames do not change (keeps `cmd/setup.go` / `cmd/uninstall.go` consistent).
- Every commit uses `git commit --signoff`. Run `go test ./...` before each commit.

## Review Focus

1. **Unstaged edits don't touch `.git/index`** — editing a tracked file without staging leaves the fingerprint unchanged, so the "N modified" count lags by up to the 30s TTL. Expected: change appears within 30s. Test: `TestCachedStatusRefreshesAfterTTL` (Task 2) with an injected clock.
2. **Linked worktree** (`.git` is a file `gitdir: ...`; `commondir` file points back) — fingerprint must use the worktree's own HEAD/index and the common dir's refs. Test: `TestResolveGitDirLinkedWorktree` (Task 1) and `TestCachedStatusLinkedWorktree` (Task 2).
3. **cwd is a subdirectory of the repo, or not in any repo** — gitdir resolution walks up parents; non-git dir must return `InGit=false, DefaultBranch="main"` with zero spawns. Tests: `TestResolveGitDirFromSubdir`, `TestGetStatusNonGitSpawnsNothing` (Task 1).
4. **Two sessions refreshing the same cwd concurrently** — no torn/corrupt cache file. Test: `TestCacheConcurrentWritersNeverCorrupt` (Task 2).
5. **cmux restarted or not answering** — a failed/empty detection must not be cached at all; env change must miss immediately. Tests: `TestDetectCachedMissesOnEnvChange`, `TestDetectCachedExpires`, `TestDetectCachedDoesNotCacheEmpty` (Task 3).
6. **Unborn repo (`git init`, no commits)** — today `rev-parse --abbrev-ref HEAD` fails there, so `Branch == ""`; porcelain v2 reports `branch.oid (initial)` with a branch name. Output must stay as today. Test: the `unborn` row of `TestGetStatusParity` (Task 1).
7. **`origin/main` moves after a fetch** — "behind" must update within the 30s status TTL, not the 10m base TTL. Test: `TestBaseCacheInvalidatesWhenBaseRefMoves` (Task 2).

---

### Task 1: Injectable git runner, filesystem gitdir resolution, porcelain v2 status

Highest impact foundation: removes the two `rev-parse --git-dir` spawns and the non-git spawn, collapses branch+porcelain into one v2 call, and lets later tests count spawns.

**Files:**
- Create: `git/runner.go`, `git/gitdir.go`, `git/porcelain.go`
- Modify: `git/status.go` (whole of `GetStatus`, `gitCmd`)
- Test: `git/gitdir_test.go`, `git/porcelain_test.go`, `git/status_test.go`

**Interfaces:**
- Produces:
  - `type Runner func(cwd string, args ...string) (string, error)` — returns trimmed stdout.
  - `var runGit Runner = execGit` (package-level, swappable); `func execGit(cwd string, args ...string) (string, error)`.
  - Tests are in-package: they assign `runGit` directly and restore it with `t.Cleanup`. Add test helper `countingRunner(t) *int` in `git/status_test.go` that wraps `execGit` and counts calls (mutex-protected).
  - `type GitDirs struct { GitDir, CommonDir, WorkTree string }`
  - `func ResolveGitDir(cwd string) (GitDirs, bool)` — pure filesystem: walk up from `cwd` to find `.git`; if dir → GitDir=CommonDir=that; if file → parse `gitdir: <path>` (relative to the file's dir), then CommonDir from `<gitdir>/commondir` (relative to GitDir) if present else GitDir. Returns false if none found.
  - `type porcelainV2 struct { Head string /* branch name or "(detached)" */; Oid string; Modified, Untracked int }`
  - `func parsePorcelainV2(out string) porcelainV2` — `# branch.oid`, `# branch.head`; lines starting `1 `, `2 `, `u ` count as Modified; `? ` as Untracked; ignore `!` and other headers.
  - `func computeBase(cwd string, dirs GitDirs, branch string) baseInfo` and `type baseInfo struct { DefaultBranch, BaseRef, MergeBase string; Ahead, Behind, CommittedAdds, CommittedDels int }` — the Phase-2 logic moved out of `GetStatus` unchanged (default branch: read `<commondir>/refs/remotes/origin/HEAD` file `ref: refs/remotes/origin/X` first, fall back to `symbolic-ref`; candidate check via `rev-parse --verify`; then merge-base, 2x rev-list, diff --shortstat in parallel).
  - `func computeStatus(cwd string, dirs GitDirs) *Status` — uncached full status: one `status --porcelain=v2 --branch --no-lock-index` + one `diff HEAD --shortstat` in parallel, rebase detection by reading files under `dirs.GitDir`, then `computeBase` when branch != default.
  - `GetStatus(cwd string) *Status` keeps its signature: `ResolveGitDir` → if !ok return `&Status{DefaultBranch:"main"}` → `computeStatus`.
- Branch mapping: v2 `branch.head == "(detached)"` → `Branch = "HEAD"` (matches today's `rev-parse --abbrev-ref HEAD` output, which the rebase logic relies on). v2 `branch.oid == "(initial)"` (unborn) → `Branch = ""` (today's value, because `rev-parse --abbrev-ref HEAD` fails there).

- [ ] **Step 0: Characterization test first, against the CURRENT code.** Create `git/status_parity_test.go` containing only `newRepo(t)` and `TestGetStatusParity` (table below; it uses nothing but `GetStatus`). Run `go test ./git/ -run TestGetStatusParity -v` → must PASS on the unmodified implementation. If a row fails, the expectation is wrong: fix the expectation, not the code. Commit it alone (`git add git/status_parity_test.go && git commit --signoff -m "git: characterize GetStatus output before refactor"`). This test must keep passing, unedited, through every later task.
- [ ] **Step 1: Write failing tests** (use `t.TempDir()` + real `git init` via `exec.Command` in the `newRepo(t) string` helper that sets `user.name/email`, makes one commit on `main`)
  - `TestResolveGitDirMainRepo`: `ResolveGitDir(repo)` returns `GitDir == repo+"/.git"`, `CommonDir == GitDir`, ok.
  - `TestResolveGitDirFromSubdir`: from `repo/a/b` returns same as above.
  - `TestResolveGitDirLinkedWorktree`: after `git worktree add ../wt -b feat`, `ResolveGitDir(wt)` → `GitDir == repo/.git/worktrees/wt` (compare via `filepath.EvalSymlinks`), `CommonDir == repo/.git`.
  - `TestResolveGitDirNonGit`: `ResolveGitDir(t.TempDir())` returns ok=false.
  - `TestParsePorcelainV2`: input with `# branch.oid abc`, `# branch.head feat`, `# branch.upstream origin/feat`, `# branch.ab +1 -2`, two `1 .M ...` lines, one `2 R. ...`, one `? new.txt` → `{Head:"feat", Oid:"abc", Modified:3, Untracked:1}`; and `# branch.head (detached)` → Head `"(detached)"`.
  - `TestGetStatusNonGitSpawnsNothing`: counting runner; `GetStatus(t.TempDir())` → `InGit==false`, `DefaultBranch=="main"`, count `== 0`.
  - `TestGetStatusParity` (written in Step 0): table of scenarios — clean on main; dirty (1 modified tracked file with 2 added lines, 1 untracked); staged rename; feature branch 2 commits ahead of `main`; feature branch with an `origin` remote whose `origin/main` is 1 commit ahead (Behind 1); detached HEAD; no upstream/no origin; linked worktree on feature branch; mid-rebase (stopped on a conflict); unborn (`git init`, no commits); cwd is a subdirectory of the repo. For each, assert `GetStatus(dir)` deep-equals the expected `Status` literal (expected values written by hand from the scenario, e.g. dirty: `Modified:1, Untracked:1, UncommittedAdds:2`; ahead: `Branch:"feat", Ahead:2, CommittedAdds:N`).
- [ ] **Step 2:** `go test ./git/ -run 'ResolveGitDir|PorcelainV2|GetStatus' -v` → FAIL (undefined: ResolveGitDir, parsePorcelainV2, runGit).
- [ ] **Step 3:** Implement the interfaces above; `gitCmd` becomes a thin wrapper over `runGit` returning `""` on error.
- [ ] **Step 4:** `go test ./git/ -v` → PASS, including the unedited `TestGetStatusParity` from Step 0.
- [ ] **Step 5:** `go test ./...` → PASS; commit:
```bash
git add git/runner.go git/gitdir.go git/porcelain.go git/status.go git/gitdir_test.go git/porcelain_test.go git/status_test.go
git commit --signoff -m "git: resolve gitdir from filesystem and use porcelain v2 status"
```

### Task 2: On-disk status cache + base cache, wired into statusline

**Files:**
- Create: `git/cache.go`
- Modify: `cmd/statusline.go:237` (call site)
- Test: `git/cache_test.go`, `cmd/statusline_git_test.go`

**Interfaces:**
- Consumes: `ResolveGitDir`, `computeStatus`, `computeBase`, `runGit` (Task 1).
- Produces:
  - `var now = time.Now` (package-level clock for tests).
  - `const statusTTL = 30 * time.Second`, `const baseTTL = 10 * time.Minute`.
  - `type fingerprint struct { HeadMtime, IndexMtime, RefMtime, PackedMtime int64; HeadSize, IndexSize int64; Rebasing bool }` — mtimes in UnixNano; missing file = 0.
  - `func takeFingerprint(dirs GitDirs) (fingerprint, string /*branch ref from HEAD file, or "" if detached*/, string /*HEAD sha*/)` — parses `<gitdir>/HEAD` itself (`ref: refs/heads/x` → stat `<commondir>/refs/heads/x`; sha read from that ref file or `packed-refs`; detached → HEAD content is the sha). No spawns.
  - `func readRefSHA(dirs GitDirs, ref string) (string, bool)` — `ref` is a full name (`refs/heads/x`, `refs/remotes/origin/main`); reads the loose file under `CommonDir`, else scans `CommonDir/packed-refs`. No spawns. `takeFingerprint` uses it for the HEAD sha.
  - `func baseRefFullName(baseRef string) string` — maps what `computeBase` chose: `"origin/main"`/`"upstream/main"` → `refs/remotes/<baseRef>`; a bare default branch → `refs/heads/<baseRef>`.
  - `func GetStatusCached(cwd, cacheDir string) *Status` — `cacheDir` e.g. `<HandlerHome>/state/git-cache`. Take the fingerprint first. Status entry file `status-<sha256(abs cwd)[:16]>.json` = `{Fingerprint, WrittenAt, Status}`; hit iff fingerprint equal and `now()-WrittenAt < statusTTL`. On miss: `computeStatus`, obtaining base info via the base entry `base-<sha256(CommonDir+"\x00"+branch)[:16]>.json` = `{HeadSHA, BaseRef, BaseRefSHA, WrittenAt, baseInfo}`; hit iff `HeadSHA` equals the HEAD sha (use the `branch.oid` from the v2 output just run), `readRefSHA(dirs, baseRefFullName(BaseRef))` succeeds and equals `BaseRefSHA`, and `now()-WrittenAt < baseTTL`; else `computeBase` and overwrite it. Store the status entry with the fingerprint taken before computing. Non-git: return uncached `&Status{DefaultBranch:"main"}`.
  - New package `internal/fscache`: `func WriteJSONAtomic(path string, v any) error` (CreateTemp in same dir + Rename; `MkdirAll` 0o755), `func ReadJSON(path string, v any) bool`, `func Prune(dir string, maxAge time.Duration, now time.Time)` (rate-limited by `<dir>/.pruned` to once per 24h; removes regular files whose mtime is older than `maxAge`; never removes `.pruned`). `GetStatusCached` calls `Prune(cacheDir, 7*24*time.Hour, now())` after a write. Tests in `internal/fscache/fscache_test.go`: `TestWriteJSONAtomicRoundTrip`, `TestReadJSONCorruptIsFalse`, `TestPruneRemovesOldKeepsNew`, `TestPruneRateLimited`.
  - Refactor `computeStatus` to take `base func() baseInfo` so the cache can inject the cached base (signature: `computeStatus(cwd string, dirs GitDirs, base func(branch string) baseInfo) *Status`; `GetStatus` passes `computeBase` directly).
- Call site: `gitStatus = gitpkg.GetStatusCached(input.CWD, filepath.Join(db.HandlerHome(), "state", "git-cache"))`.

- [ ] **Step 1: Failing tests** (`git/cache_test.go`, reusing `newRepo` and counting runner, `now` overridden with a settable fake)
  - `TestCachedStatusWarmHitSpawnsNothing`: first call count > 0; second call count delta `== 0`; results deep-equal.
  - `TestCachedStatusInvalidatesOnCommit`: commit (with real git outside the runner) → next call result has updated ahead/branch state and delta > 0.
  - `TestCachedStatusInvalidatesOnStage`: `git add` a new file → `Modified` increases on next call.
  - `TestCachedStatusInvalidatesOnCheckout`: `git checkout -b other` → `Branch == "other"`.
  - `TestCachedStatusRefreshesAfterTTL`: modify tracked file without staging; call with fake clock +29s → old `Modified`; +31s → new `Modified`.
  - `TestBaseCacheReusedAcrossDirtyChanges`: on a feature branch, stage a file → status recomputed but count of `merge-base` invocations (runner filters on args[0]) stays at 1.
  - `TestBaseCacheExpiresAfter10m`: fake clock +11m with fresh status miss → `merge-base` count 2.
  - `TestBaseCacheInvalidatesWhenBaseRefMoves`: feature branch with `origin/main`; warm; move `refs/remotes/origin/main` forward one commit (`git update-ref`, outside the runner); fake clock +31s → `Behind` increases by 1 and `merge-base` count is 2.
  - `TestBaseCacheOneFilePerBranch`: make 3 commits on the same branch with a status miss after each → exactly one `base-*.json` file in the cache dir.
  - `TestFingerprintTakenBeforeCompute`: runner wrapper that makes a commit (via `execGit`) the first time it sees `status`; the call after that one must be a miss (delta > 0) and return the post-commit state.
  - `TestCachedStatusLinkedWorktree`: worktree on `feat`, warm hit spawns 0; committing in the worktree invalidates.
  - `TestCacheConcurrentWritersNeverCorrupt`: 20 goroutines × 10 `GetStatusCached` on same cwd with fresh clock each; afterwards `fscache.ReadJSON` on the status file succeeds and every result equals the expected `Status`.
  - `TestCorruptCacheFileIsMiss`: write `"{"` to the status file → call returns correct status, no panic.
  - `cmd/statusline_git_test.go` `TestFormatGitLineUnchanged`: golden strings for `formatGitLine` on Status values for clean-main, dirty, ahead+behind, detached (`Branch:"HEAD"`), rebasing — strings captured from the current `formatGitLine` BEFORE any change in this task (run once, paste literals).
- [ ] **Step 2:** `go test ./git/ ./cmd/ -run 'Cached|BaseCache|Cache|FormatGitLine' -v` → FAIL (undefined GetStatusCached).
- [ ] **Step 3:** Implement `git/cache.go` per Interfaces; update call site.
- [ ] **Step 4:** `go test ./git/ ./cmd/ -v` → PASS.
- [ ] **Step 5:** `go test ./...`; commit:
```bash
git add git/cache.go git/status.go git/cache_test.go internal/fscache/fscache.go internal/fscache/fscache_test.go cmd/statusline.go cmd/statusline_git_test.go
git commit --signoff -m "git: cache statusline git status per cwd and base info per HEAD"
```

### Task 3: Cache terminal detection per session

**Files:**
- Create: `terminal/detect_cache.go`
- Modify: `cmd/statusline.go:178`, `cmd/user_prompt_submit.go:100`
- Test: `terminal/detect_cache_test.go`

**Interfaces:**
- Produces:
  - `var detectFn = Detect` and `var now = time.Now` (package-level, swappable).
  - `const detectTTL = 5 * time.Minute`
  - `func DetectCached(cacheDir, sessionID string) (backendType, terminalID, workspaceID string)` — file `<cacheDir>/<sha256(sessionID)[:16]>.json` = `{Surface, Tmux string; WrittenAt time.Time; Type, ID, Workspace string}`; hit iff `Surface == os.Getenv("CMUX_SURFACE_ID")`, `Tmux == os.Getenv("TMUX")`, and age < `detectTTL`; else call `detectFn()` and write with `fscache.WriteJSONAtomic`, then `fscache.Prune(cacheDir, 7*24*time.Hour, now())`. Do not write when `terminalID == ""`, or when the type is cmux and `workspaceID == ""` (a failed `cmux identify` must be retried next call). Empty `sessionID` → call `detectFn()` uncached.
  - Consequence to keep in mind: if a surface is moved to another cmux workspace, the session's stored workspace id (used by `handler switch`) can be up to 5 minutes stale.
- Call sites pass `filepath.Join(db.HandlerHome(), "state", "terminal-cache")` and `input.SessionID`. Registration paths (`statusline.go:1180,1235`, `user_prompt_submit.go:151`) stay on `Detect()`.

- [ ] **Step 1: Failing tests** (fake `detectFn` counting calls; `t.Setenv`)
  - `TestDetectCachedHit`: two calls within TTL → `detectFn` called once, same triple.
  - `TestDetectCachedMissesOnEnvChange`: change `CMUX_SURFACE_ID` between calls → called twice, second triple reflects new fake.
  - `TestDetectCachedExpires`: fake clock +4m59s → called once; +5m1s → called twice.
  - `TestDetectCachedDoesNotCacheEmpty`: fake returns `("cmux", "surface:1", "")` → two calls invoke it twice and no file is written; same for `("", "", "")`.
  - `TestDetectCachedPerSession`: sessions "a" and "b" → called twice.
  - `TestDetectCachedEmptySessionUncached`: `sessionID==""` twice → called twice, no files in cacheDir.
- [ ] **Step 2:** `go test ./terminal/ -run DetectCached -v` → FAIL.
- [ ] **Step 3:** Implement and update the two call sites.
- [ ] **Step 4:** `go test ./terminal/ ./cmd/ -v` → PASS.
- [ ] **Step 5:** `go test ./...`; commit:
```bash
git add terminal/detect_cache.go terminal/detect_cache_test.go cmd/statusline.go cmd/user_prompt_submit.go
git commit --signoff -m "terminal: cache cmux/tmux detection per session for statusline and prompt hooks"
```

### Task 4: Syscall-based process liveness and command-line lookup

**Files:**
- Modify: `discover/pid.go:60-100`, `go.mod` (x/sys direct)
- Create: `discover/cmdline_darwin.go`, `discover/cmdline_linux.go`, `discover/cmdline_other.go` (`//go:build !darwin && !linux`)
- Test: `discover/pid_test.go`

**Interfaces:**
- Produces:
  - `IsProcessAlive(pid int) bool` (signature unchanged) — `syscall.Kill(pid, 0)`; alive iff `err == nil || errors.Is(err, syscall.EPERM)`; `pid <= 0` → false.
  - `func processCmdline(pid int) (string, error)` — darwin: `unix.SysctlRaw("kern.procargs2", pid)`, skip 4-byte argc, exec path, NUL padding, then join the next argc NUL-separated args with spaces; linux: read `/proc/<pid>/cmdline`, replace NUL with space; other: `ps -o command= -p` (current behavior).
  - `IsSessionProcess` keeps its signature and logic; only the `ps` call becomes `processCmdline`.

- [ ] **Step 1: Failing tests**
  - `TestIsProcessAliveSelf`: `IsProcessAlive(os.Getpid())` true; `IsProcessAlive(0)` false; spawn+wait `sleep 0` child then `IsProcessAlive(childPid)` false.
  - `TestProcessCmdlineSelf`: `processCmdline(os.Getpid())` contains `filepath.Base(os.Args[0])` and, for an arg passed via `exec.Command("sleep","30")` child, `processCmdline(child)` == `"sleep 30"` (kill child in Cleanup).
  - `TestIsSessionProcessMatchesCmdline`: start child `bash -c 'exec -a claude sleep 30' --` is unreliable; instead start `sleep 30` and assert `IsSessionProcess(pid, "x") == false` (no "claude" in cmdline) — pins that the cmdline path still runs.
- [ ] **Step 2:** `go test ./discover/ -v` → FAIL (undefined processCmdline).
- [ ] **Step 3:** Implement; `go mod tidy` (moves x/sys to direct).
- [ ] **Step 4:** `go test ./discover/ -v` → PASS.
- [ ] **Step 5:** `go test ./...`; commit:
```bash
git add discover/pid.go discover/cmdline_darwin.go discover/cmdline_linux.go discover/cmdline_other.go discover/pid_test.go go.mod go.sum
git commit --signoff -m "discover: check liveness and read cmdline via syscalls instead of kill/ps"
```

### Task 5: Lighter hook wrappers

**Files:**
- Modify: `hooks/post_tool_use_wake.sh`, `hooks/statusline.sh`
- Test: `cmd/hook_scripts_test.go` (new; runs the embedded scripts with `bash`)

**Interfaces:**
- `post_tool_use_wake.sh`: do NOT parse JSON in bash (a regex for `"session_id"` takes the leftmost match, which can be a nested key inside `tool_input`, and a wrong id silently skips the wake). New logic, no stdin read and no spawn in the common case:
  ```bash
  DIR="${HANDLER_HOME:-$HOME/.agent-handler}/state/wake-armed"
  shopt -s nullglob
  armed=("$DIR"/*)
  [ ${#armed[@]} -gt 0 ] || exit 0
  handler wake-check 2>/dev/null || true   # stdin inherited; Go parses the payload
  exit 0
  ```
  Before relying on this, read `handler wake-check` (`cmd/wake.go`) and confirm it exits quietly, doing nothing, when the payload's session has no marker. If it does not, add that guard in Go with a test (`TestWakeCheckNoopWhenSessionNotArmed`) in this task.
- `statusline.sh`: replace `cat | handler statusline --from-hook 2>/dev/null` with `exec handler statusline --from-hook 2>/dev/null` (stdin inherited; `CLAUDE_PID=$PPID` export preserved — `exec` keeps the shell's PPID semantics since `$PPID` is evaluated before exec).
- Filenames unchanged, so `configureHooks()` / `removeHooks()` need no edit.

- [ ] **Step 1: Failing tests** (write scripts from repo `hooks/` dir; put a fake `handler` script on a temp `PATH` that writes its stdin and args to a file; set `HANDLER_HOME` to temp dir)
  - `TestWakeHookNoMarkerDoesNotInvokeHandler`: payload `{"session_id":"abc","tool_name":"Bash"}`, `wake-armed` dir empty (and a second case: dir missing) → exit 0, fake handler never ran.
  - `TestWakeHookWithMarkerPassesPayload`: create `state/wake-armed/abc` → fake handler received args `wake-check` and the exact payload bytes.
  - `TestWakeHookNestedSessionIdKey`: marker for `abc`; payload `{"tool_input":{"session_id":"zzz"},"session_id":"abc"}` → handler invoked with the full payload (this is the case a bash regex gets wrong).
  - `TestWakeHookHandlerFailureIsInvisible`: fake handler exits 1 → script exits 0.
  - `TestWakeHookDoesNotUseJq`: script source contains no `jq`, no `cat`, no `sed` (string assertion; this is the regression guard).
  - `TestStatuslineHookPassesStdin`: fake handler gets args `statusline --from-hook`, stdin bytes intact, and env `CLAUDE_PID` non-empty.
- [ ] **Step 2:** `go test ./cmd/ -run 'WakeHook|StatuslineHook' -v` → `TestWakeHookDoesNotUseJq` FAILS (others pass on old script — they pin behavior).
- [ ] **Step 3:** Edit both scripts.
- [ ] **Step 4:** `go test ./cmd/ -run 'WakeHook|StatuslineHook' -v` → PASS.
- [ ] **Step 5:** `go test ./...`; commit:
```bash
git add hooks/post_tool_use_wake.sh hooks/statusline.sh cmd/hook_scripts_test.go
git commit --signoff -m "hooks: drop jq and cat spawns from per-tool-call and statusline wrappers"
```

### Task 6: Spawn-count guard for the warm statusline path

**Files:**
- Test: `git/spawn_budget_test.go`, `terminal/detect_cache_test.go` (add one test)
- Create (if needed): none

**Interfaces:**
- Consumes: `runGit`, `GetStatusCached`, `now` (Task 2); `detectFn`, `DetectCached` (Task 3).

- [ ] **Step 1: Write tests**
  - `TestWarmStatuslineSpawnsZeroGit` (table over scenarios from Task 1: clean main, dirty, feature ahead, detached, no-upstream, linked worktree): warm once, reset counter, call `GetStatusCached` 5× → counter `== 0`; and result equals uncached `GetStatus` result.
  - `TestColdStatuslineSpawnBudget`: cold call on feature-ahead scenario with empty cache dir → counter `<= 7` (v2 status, diff HEAD, rev-parse --verify ×≤2, merge-base, rev-list ×2... asserts the actual number measured; record it as the constant `coldSpawnBudget` in the test so future regressions are visible). Second cold status miss (stage a file) with warm base cache → counter `== 2`.
  - `BenchmarkGetStatusCachedWarm` and `BenchmarkGetStatusUncached` (report `b.ReportMetric(float64(spawns)/float64(b.N), "spawns/op")`).
  - `TestDetectCachedWarmSpawnsNothing`: with real `Detect` replaced by a counting fake, 5 warm calls → 0.
- [ ] **Step 2:** `go test ./git/ ./terminal/ -run 'Spawn|Warm' -v` → PASS (tasks 2-3 already provide the behavior; if any fails, fix the owning code, not the test).
- [ ] **Step 3:** `go test ./git/ -bench GetStatus -run '^$'` → warm `spawns/op` 0.
- [ ] **Step 4:** End-to-end check with an isolated binary and home — do NOT run `make install`. `go build -o "$TMP/handler" .` into a fresh temp dir; create `$TMP/shim/git` and `$TMP/shim/cmux` scripts that append their args to `$TMP/spawns.log` and then `exec` the real binary (resolve the real paths before changing `PATH`). In a git repo run `echo '{"session_id":"x","cwd":"'$PWD'"}' | HANDLER_HOME="$TMP/home" PATH="$TMP/shim:$PATH" "$TMP/handler" statusline --from-hook` twice. Expect the second run to add 0 `git` lines and 0 `cmux` lines to `spawns.log`. Report both counts. If the command needs more input fields to run, take them from an existing statusline test fixture.
- [ ] **Step 5:** `go test ./...` → PASS; commit:
```bash
git add git/spawn_budget_test.go terminal/detect_cache_test.go
git commit --signoff -m "test: guard zero git spawns on warm statusline path"
```

## Deployment (user-run, after merge — not an implementer step)

- `make install` puts the new binary and hook scripts in place. It stops running `handler ui`, `handler watcher` and `handler tail` servers, so the user runs it and restarts those himself.
- Sessions pick up the new statusline on their next refresh; hook script changes apply to the next tool call.

## Out of scope (noted, not planned)

- `handler ui`'s 5s ticker runs `cmux workspace list` / `list-pane-surfaces` while the UI is open.
- `registerSession`'s 2 git calls (first prompt only) and the rate-limited Slack name sync.
