# Phase 2c: Legacy Watcher Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all legacy-watcher backwards-compat from agent-handler — the four legacy tables, the legacy inbox query path, and every remaining direct read of those tables — leaving the watcher library's `watcher_*` tables as the single source of truth, with `handler setup --migrate-watcher` as the sole, self-contained code path that touches the legacy schema.

**Architecture:** Phase 2b integrated the watcher library behind a `watcher_migrated` marker while *retaining* the old tables + a legacy inbox arm as a safety net. This phase removes that net. It finishes the read-repointing 2b deferred (resource relationships, resource state, remaining `subscriptions` reads), deletes the marker-gated legacy inbox arm so the UNION path is the only path, restructures the migration command to own the full legacy lifecycle (run legacy migrations → copy → drop legacy tables) and be idempotent, and stops fresh installs from ever creating the legacy tables. The `resource_relationships` feature is genuinely handler-owned (not dead), so it is moved onto the library via a new v0.2.3 read API rather than dropped.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite` (pure Go), cobra CLI, `github.com/mturley/watcher` library (currently pinned v0.2.2; this plan bumps to v0.2.3).

## Global Constraints

- **Two repos.** Library: `github.com/mturley/watcher` at `/Users/mturley/git/watcher` (branch `main`). Handler: `github.com/mturley/agent-handler` at the worktree `/Users/mturley/.worktrees/agent-ledger/phase2c-cleanup` (branch `phase2c-cleanup`). During development handler uses a go.mod **local `replace`** to the library checkout; the final task drops the replace and pins the tagged v0.2.3.
- **NEVER touch the real database** `~/.agent-handler/handler.db` or the real `~/.config/watcher/*.yaml`. Every test uses `t.TempDir()` and, for any credential/config-path code, isolates BOTH `HANDLER_HOME` and `WATCHER_HOME` env vars (helper `isolateHomes(t)` already exists in `cmd/migrate_watcher_test.go`).
- **The migration must remain runnable and correct.** `handler setup --migrate-watcher` is retained (not deleted). It becomes the *only* code that references the legacy tables.
- **`watcher_migrated` marker is being retired** as the legacy-detection signal. Legacy detection becomes schema-based (does a legacy table exist?). Before deleting the marker, verify no remaining consumer needs it (the inbox gate — the other consumer — is removed in Task 5, so ordering matters: Task 5 removes the gate, Task 6 restructures migration/detection, and the marker is retired once both are done).
- **Keep** `watcher/scheduler.go` (handler's launchd/cron scheduling) and the library's `watcher_schema_version` table. Delete only handler's legacy code/tables.
- **`git add` specific files by name** (never `git add -A`/`.`); commit with `--signoff`; keep `go build ./... && go vet ./...` clean and `go test ./...` green at the end of every task. The one deliberately destructive step (purging `events` rows) is the final task, gated behind everything else being green.
- The four legacy tables: `subscriptions`, `resource_state`, `resource_relationships`, `watcher_status`.

---

## File Structure

**Library (`/Users/mturley/git/watcher`):**
- `db/relationships.go` — add `SiblingResources` read function (currently write-only: `LinkResources`).
- `db/relationships_test.go` (or existing `db/resourcestate_test.go` which holds `TestLinkResourcesIdempotent`) — add sibling-query tests.

**Handler (`/Users/mturley/.worktrees/agent-ledger/phase2c-cleanup`):**
- `db/resources.go` — repoint `FindRelatedSessions` sibling traversal + `LinkResources` to the library; drop direct `resource_relationships` SQL.
- `db/resource_state.go` — delete (dead after repointing the last reader).
- `cmd/api/resources.go` — repoint `GetResourceState` call to library.
- `cmd/api/events.go`, `cmd/api/sessions.go` — repoint `subscriptions`/`resource_state` JOINs/reads to library-backed data.
- `cmd/statusline.go`, `cmd/watching.go`, `cmd/health.go`, `cmd/unregister.go`, `cmd/user_prompt_submit.go` — repoint remaining direct `subscriptions` reads to library-backed helpers.
- `db/subscriptions.go` — remove legacy-table SQL left after 2b (verify what remains).
- `db/inbox_scope.go`, `db/events.go`, `db/cursors.go` — remove the gate + legacy single-query arm; the UNION becomes the only path.
- `cmd/migrate_watcher.go` — absorb legacy `runMigrations` steps; drop legacy tables at end; idempotent-on-absence.
- `db/db.go` — `runMigrations` becomes a documented no-op; `Open` no longer creates legacy tables.
- `db/schema.sql` — remove the four legacy `CREATE TABLE` blocks (+ their indexes).
- `db/legacy_guard.go` — rewrite `HasUnmigratedLegacyData` to be schema-based.
- `README.md`, `docs/watcher-migration-runbook.md` — update.

---

### Task 1: Library v0.2.3 — `SiblingResources` read API

**Repo:** `/Users/mturley/git/watcher` (branch `main`).

**Files:**
- Modify: `db/relationships.go`
- Test: `db/relationships_test.go` (create) — or append to `db/resourcestate_test.go` if the repo keeps relationship tests there; prefer a dedicated `relationships_test.go`.

**Interfaces:**
- Consumes: `watcher.Resource{Type, ID, URL string}` (already defined in the library's root package, imported as `watcher`); the existing `watcher_resource_relationships` table (columns: `id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at`).
- Produces: `func SiblingResources(conn *sql.DB, resource watcher.Resource) ([]watcher.Resource, error)` — returns the distinct child resources that share at least one parent with `resource`, excluding `resource` itself. Each returned `watcher.Resource` has `Type` and `ID` set (URL left empty — callers only need type+id).

- [ ] **Step 1: Write the failing test**

Create `db/relationships_test.go`:

```go
package db

import (
	"testing"

	watcher "github.com/mturley/watcher"
)

func TestSiblingResources(t *testing.T) {
	conn := mem(t) // existing helper in db package tests (see migrate_test.go:10)
	if err := Migrate(conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	epic := watcher.Resource{Type: "jira", ID: "EPIC-1"}
	childA := watcher.Resource{Type: "jira", ID: "TASK-1"}
	childB := watcher.Resource{Type: "jira", ID: "TASK-2"}
	unrelated := watcher.Resource{Type: "jira", ID: "TASK-9"}
	otherEpic := watcher.Resource{Type: "jira", ID: "EPIC-2"}

	// TASK-1 and TASK-2 share parent EPIC-1; TASK-9 is under EPIC-2.
	if err := LinkResources(conn, childA, epic, "epic", "test"); err != nil {
		t.Fatalf("link A: %v", err)
	}
	if err := LinkResources(conn, childB, epic, "epic", "test"); err != nil {
		t.Fatalf("link B: %v", err)
	}
	if err := LinkResources(conn, unrelated, otherEpic, "epic", "test"); err != nil {
		t.Fatalf("link unrelated: %v", err)
	}

	got, err := SiblingResources(conn, childA)
	if err != nil {
		t.Fatalf("SiblingResources: %v", err)
	}
	// Expect exactly TASK-2 (shares EPIC-1), NOT TASK-1 (self), NOT TASK-9.
	if len(got) != 1 || got[0].Type != "jira" || got[0].ID != "TASK-2" {
		t.Fatalf("SiblingResources(TASK-1) = %v, want [jira/TASK-2]", got)
	}

	// A resource with no relationships has no siblings.
	none, err := SiblingResources(conn, watcher.Resource{Type: "jira", ID: "NOPE"})
	if err != nil {
		t.Fatalf("SiblingResources(NOPE): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("SiblingResources(NOPE) = %v, want empty", none)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/mturley/git/watcher && go test ./db/ -run TestSiblingResources -v`
Expected: FAIL — `undefined: SiblingResources`.

- [ ] **Step 3: Implement `SiblingResources`**

Add to `db/relationships.go` (mirror the query in handler's current `FindRelatedSessions`, `db/resources.go:79-84`):

```go
// SiblingResources returns the distinct child resources that share at least one
// parent with the given resource, excluding the resource itself. Used to find
// "related" resources (e.g. Jira issues under the same epic) without exposing
// the relationship table's shape to consumers.
func SiblingResources(conn *sql.DB, resource watcher.Resource) ([]watcher.Resource, error) {
	rows, err := conn.Query(`
		SELECT DISTINCT rr_other.child_type, rr_other.child_id
		FROM watcher_resource_relationships rr_mine
		JOIN watcher_resource_relationships rr_other
		  ON rr_other.parent_type = rr_mine.parent_type AND rr_other.parent_id = rr_mine.parent_id
		WHERE rr_mine.child_type = ? AND rr_mine.child_id = ?
		  AND (rr_other.child_type != rr_mine.child_type OR rr_other.child_id != rr_mine.child_id)
	`, resource.Type, resource.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sibling resources of %s/%s: %w", resource.Type, resource.ID, err)
	}
	defer rows.Close()

	var siblings []watcher.Resource
	for rows.Next() {
		var t, id string
		if err := rows.Scan(&t, &id); err != nil {
			return nil, fmt.Errorf("failed to scan sibling resource: %w", err)
		}
		siblings = append(siblings, watcher.Resource{Type: t, ID: id})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sibling resources: %w", err)
	}
	return siblings, nil
}
```

Ensure `fmt` is imported in `db/relationships.go` (it already imports it for `LinkResources`; verify).

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/mturley/git/watcher && go test ./db/ -run TestSiblingResources -v`
Expected: PASS.

- [ ] **Step 5: Full library verify**

Run: `cd /Users/mturley/git/watcher && go build ./... && go vet ./... && go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit + tag v0.2.3**

```bash
cd /Users/mturley/git/watcher
git add db/relationships.go db/relationships_test.go
git commit -s -m "feat(db): SiblingResources read API for shared-parent relationships"
git tag -a v0.2.3 -m "watcher v0.2.3: SiblingResources read API"
git push origin main
git push origin v0.2.3
```

(Controller: tagging/pushing the library is pre-authorized for this session per the standing approval. If unsure, confirm before the push.)

---

### Task 2: Handler — repoint relationships to the library, drop `resource_relationships`

**Repo:** handler worktree. Uses a go.mod local `replace` to `/Users/mturley/git/watcher` so the just-committed `SiblingResources` is available immediately (the v0.2.3 pin happens in Task 8).

**Files:**
- Modify: `go.mod` (add local replace if not already present)
- Modify: `db/resources.go` (`LinkResources`, `FindRelatedSessions` sibling block)
- Test: `db/resources_test.go` (update `TestLinkAndFindRelated` if present)

**Interfaces:**
- Consumes: `wdb.LinkResources(conn, child, parent watcher.Resource, relationship, source string) error`; `wdb.SiblingResources(conn, resource watcher.Resource) ([]watcher.Resource, error)` (Task 1). `watcherlib "github.com/mturley/watcher"` for `watcher.Resource`.
- Produces: `(*db.DB).LinkResources(r ResourceRelationship) error` (SIGNATURE UNCHANGED — delegates to the library); `FindRelatedSessions(sessionID string) ([]Session, error)` (UNCHANGED — sibling traversal now via `wdb.SiblingResources`). Handler's `ResourceRelationship` struct stays as the public handler-side type.

- [ ] **Step 1: Ensure the local replace is in go.mod**

Run:
```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
go mod edit -replace github.com/mturley/watcher=/Users/mturley/git/watcher
go mod tidy
grep -n "replace github.com/mturley/watcher" go.mod
```
Expected: the replace line is present. (Do NOT commit go.mod yet if it only contains the replace — Task 8 removes it. But `go mod tidy` may update go.sum; that's fine, it'll be reconciled at Task 8.)

- [ ] **Step 2: Update the test**

In `db/resources_test.go`, find `TestLinkAndFindRelated` (or the test covering `LinkResources`+`FindRelatedSessions`). Ensure it seeds via `LinkResources` (handler method) + subscriptions via the library, then asserts `FindRelatedSessions` returns the session subscribed to a sibling resource. If the test currently seeds handler's `resource_relationships` table directly, change it to use `d.LinkResources(...)`. Run it first to see current state:

Run: `cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup && go test ./db/ -run TestLinkAndFindRelated -v`

If it passes today (it should — 2b left this working via the handler table), that's the behavior to PRESERVE after repointing.

- [ ] **Step 3: Repoint `LinkResources`**

In `db/resources.go`, replace the body of `(*db.DB) LinkResources` (currently `INSERT INTO resource_relationships ...`) with a delegation to the library:

```go
// LinkResources records a hierarchical relationship between two resources in
// the watcher library's watcher_resource_relationships table.
func (db *DB) LinkResources(r ResourceRelationship) error {
	child := watcherlib.Resource{Type: r.ChildType, ID: r.ChildID, URL: derefStr(r.ChildURL)}
	parent := watcherlib.Resource{Type: r.ParentType, ID: r.ParentID, URL: derefStr(r.ParentURL)}
	if err := wdb.LinkResources(db.conn, child, parent, r.Relationship, r.Source); err != nil {
		return fmt.Errorf("failed to link resources: %w", err)
	}
	return nil
}

// derefStr returns the pointed-to string, or "" if the pointer is nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
```

CONFIRMED field types on handler's `ResourceRelationship` struct (`db/resources.go:11`): `ID, ChildType, ChildID string; ChildURL *string; ParentType, ParentID string; ParentURL *string; Relationship, Source, CreatedAt string`. `ChildURL`/`ParentURL` are `*string`, so they MUST be dereferenced (via `derefStr` above) — the library's `watcher.Resource.URL` is a plain `string`. If a `deref`/`derefStr` helper already exists in package `db` (2b added one in `db/watcher_bridge.go` — grep first), reuse it instead of redefining.

- [ ] **Step 4: Repoint the sibling traversal in `FindRelatedSessions`**

In `db/resources.go`, `FindRelatedSessions`, replace the inline `db.conn.Query(\`SELECT DISTINCT rr_other... FROM resource_relationships ...\`)` block (the sibling lookup, roughly `db/resources.go:78-104`) with a call to the library:

```go
		// Sessions subscribed to a sibling resource sharing a parent.
		siblings, err := wdb.SiblingResources(db.conn, watcherlib.Resource{Type: r.Resource.Type, ID: r.Resource.ID})
		if err != nil {
			return nil, fmt.Errorf("failed to find sibling resources of %s/%s: %w", r.Resource.Type, r.Resource.ID, err)
		}
		for _, sib := range siblings {
			if err := addSubscribers(sib.Type, sib.ID); err != nil {
				return nil, err
			}
		}
```

This removes the entire `siblingRows` scan loop. Keep the surrounding `for _, r := range mine` loop, `addSubscribers`, and the final session-loading block unchanged.

- [ ] **Step 5: Confirm no `resource_relationships` SQL remains in handler outside the migration**

Run:
```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
grep -rn --include="*.go" "resource_relationships" cmd/ db/ | grep -v "_test.go\|migrate_watcher.go\|legacy_guard.go"
```
Expected: NO matches (all live reads/writes now go through the library). `legacy_guard.go` and `migrate_watcher.go` still reference it — that's correct (they handle the legacy table).

- [ ] **Step 6: Verify**

Run: `cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup && go build ./... && go vet ./... && go test ./db/ -run TestLinkAndFindRelated -v && go test ./...`
Expected: build/vet clean; `TestLinkAndFindRelated` PASS; full suite PASS.

- [ ] **Step 7: Commit**

```bash
git add db/resources.go db/resources_test.go
git commit -s -m "refactor(db): relationships via watcher library (LinkResources + SiblingResources)"
```

---

### Task 3: Handler — repoint remaining `resource_state` reads, delete dead `db/resource_state.go`

**Files:**
- Modify: `cmd/api/resources.go` (the `s.DB.GetResourceState(...)` call, ~line 115)
- Modify: `cmd/api/events.go` (the `LEFT JOIN resource_state rs` at ~line 171)
- Delete: `db/resource_state.go` (handler's `ResourceState` struct + `UpsertResourceState`/`GetResourceState`/`DeleteResourceState`) — ONLY if no live caller remains after repointing. `ListResourceStatesForSession` was already repointed to the library in 2b (Task 4b) and lives in `db/resource_state.go` too — CHECK: if `ListResourceStatesForSession` is still there and still used, keep the file but delete only the now-dead legacy-table methods. (Read the file first.)
- Test: `db/resource_state_test.go` (adjust/remove tests for deleted methods)

**Interfaces:**
- Consumes: `wdb.GetResourceState(conn, resourceType, resourceID string) (*wdb.ResourceState, error)` — returns `nil, nil` when absent; `wdb.ResourceState{ResourceType, ResourceID, StateJSON, ResourceUpdatedAt, WatcherUpdatedAt string}`.
- Produces: no new handler API; `cmd/api/resources.go` reads state via the library.

- [ ] **Step 1: Read `db/resource_state.go` and enumerate callers**

Run:
```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
grep -rn --include="*.go" "\.GetResourceState\|\.UpsertResourceState\|\.DeleteResourceState\|ListResourceStatesForSession" cmd/ db/ | grep -v "_test.go"
```
Record which handler methods still have live callers. Expectation from survey: only `cmd/api/resources.go:115` calls the handler `GetResourceState`; `Upsert`/`Delete` have no live callers; `ListResourceStatesForSession` is already library-backed (keep it).

- [ ] **Step 2: Repoint `cmd/api/resources.go`**

Change the `state, err := s.DB.GetResourceState(entry.ResourceType, entry.ResourceID)` call to use the library. Add import `wdb "github.com/mturley/watcher/db"` if absent, and use the DB's underlying conn accessor (`s.DB.Conn()`):

```go
		state, err := wdb.GetResourceState(s.DB.Conn(), entry.ResourceType, entry.ResourceID)
```

The library's `*wdb.ResourceState` has the same field names (`StateJSON`, `ResourceUpdatedAt`, `WatcherUpdatedAt`) as handler's struct, and returns `nil` when absent — verify the downstream usage handles `nil` (the loop `continue`s on error; a `nil` state should be treated as "no cached state", same as before). Read the surrounding block (~`cmd/api/resources.go:112-125`) and preserve its nil/empty handling.

- [ ] **Step 3: Repoint `cmd/api/events.go` resource_state JOIN**

The `LEFT JOIN resource_state rs ON ...` at ~`cmd/api/events.go:171` enriches events with cached state. Determine what columns it pulls (likely `rs.state_json`). Repoint the data source to `watcher_resource_state`. Since this is a SQL JOIN (not a single lookup), the cleanest change is to change the joined table name `resource_state` → `watcher_resource_state` (identical columns). VERIFY the columns referenced exist identically in `watcher_resource_state` (they do: `resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at`). Make the same table-name substitution.

- [ ] **Step 4: Delete dead legacy methods**

If Step 1 confirmed `UpsertResourceState`/`GetResourceState`/`DeleteResourceState` (the handler versions that hit the legacy `resource_state` table) now have zero live callers, delete those three methods and the handler `ResourceState` struct from `db/resource_state.go`. KEEP `ListResourceStatesForSession` and `ResourceStateWithSubscription` (library-backed, still used). Remove now-unused imports.

- [ ] **Step 5: Update tests**

In `db/resource_state_test.go`, remove tests that exercised the deleted legacy methods (`TestUpsertAndGetResourceState` etc. if present). Keep `TestListResourceStatesForSession`.

- [ ] **Step 6: Confirm no legacy `resource_state` SQL remains outside migration**

Run:
```bash
grep -rn --include="*.go" "\bresource_state\b" cmd/ db/ | grep -v "_test.go\|migrate_watcher.go\|legacy_guard.go\|watcher_resource_state"
```
Expected: NO matches.

- [ ] **Step 7: Verify + commit**

Run: `go build ./... && go vet ./... && go test ./...` → all PASS.
```bash
git add cmd/api/resources.go cmd/api/events.go db/resource_state.go db/resource_state_test.go
git commit -s -m "refactor: read resource state from watcher library, drop legacy resource_state code"
```

---

### Task 4: Handler — repoint remaining direct `subscriptions` reads

**Files (all read the legacy `subscriptions` table directly and must move to library-backed data):**
- Modify: `cmd/statusline.go` (`FROM subscriptions` at ~730 and ~1156)
- Modify: `cmd/watching.go` (~169, ~186)
- Modify: `cmd/health.go` (~66, ~70 — subscription counts)
- Modify: `cmd/unregister.go` (~62-65 — soft-delete subscriptions)
- Modify: `cmd/user_prompt_submit.go` (~179 — migrate subscriptions from archived same-name session)
- Modify: `cmd/api/events.go` (`JOIN subscriptions sub` at ~73)
- Modify: `cmd/api/resources.go` (`FROM subscriptions s` at ~47)
- Test: the corresponding `_test.go` files for each behavior touched.

**Interfaces (library, package `wdb "github.com/mturley/watcher/db"`; handler already wraps most of these — prefer existing handler methods that are already library-backed):**
- `wdb.ActiveSubscriptions(conn, subscriber string, prefix bool) ([]wdb.Subscription, error)` — live subs for a subscriber.
- `wdb.SubscribersOf(conn, resourceType, resourceID string) ([]wdb.Subscription, error)` — who subscribes to a resource.
- Handler helpers already over the library (from 2b, in `db/subscriptions.go`/`db/watcher_bridge.go`): `ListSubscriptions`, `RestoreSubscriptionsForSession`, `RenewSubscriptionsForSession`, `SoftDeleteSubscriptionsForSession`, `handlerSubscriber(sessionID)`. PREFER these over raw SQL.

> NOTE: This task is a *sweep*. For EACH call site, read what it computes and replace the raw `subscriptions` query with the equivalent library-backed handler method (or a direct `wdb.*` call via `d.Conn()`), preserving exact output. Do them one file at a time, running that file's tests after each. Below are the per-site specifications.

- [ ] **Step 1: `cmd/health.go` subscription counts**

`SELECT COUNT(*) FROM subscriptions` (total) and `... WHERE deleted_at IS NULL` (active). Replace with library-backed counts. There is no single "count all subscriptions across all subscribers" library function, so count via `wdb.ActiveSubscriptions` per the handler-subscriber prefix is wrong (that's per-session). Instead, add a small library-agnostic count using `d.Conn()` against `watcher_subscriptions`:
- total: `SELECT COUNT(*) FROM watcher_subscriptions`
- active: `SELECT COUNT(*) FROM watcher_subscriptions WHERE deleted_at IS NULL AND (expires_at IS NULL OR expires_at > ?)` binding `time.Now().UTC().Format(time.RFC3339)`.

Write the two queries against `watcher_subscriptions` in `cmd/health.go`. (This is a health readout; matching the library's live-subscription predicate for "active" is correct.) Update `cmd/health_test.go` if it asserts these counts (seed via the library).

- [ ] **Step 2: `cmd/watching.go` (~169, ~186)**

Read both queries. `handler watching` lists watched resources for display. Determine the scope (all sessions? current session?). Replace with the equivalent over `watcher_subscriptions` via `d.Conn()` or an existing handler method. If it lists per-session, use `ListSubscriptions(sessionID, includeDeleted)`. If it lists globally with session attribution, query `watcher_subscriptions` directly and parse the session id from `subscriber` via `sessionIDFromSubscriber` (in `db/watcher_bridge.go`). Preserve the displayed columns exactly. Update `cmd/watching_test.go`.

- [ ] **Step 3: `cmd/statusline.go` (~730, ~1156)**

Read both. ~1156 is in the archived-session subscription/cursor migration path ("Migrate subscriptions and cursor from archived session with same name", comment at ~1075). This should use the handler's already-library-backed restore/renew methods rather than raw `subscriptions` SQL. ~730 is likely a per-session subscription read for statusline display. Replace each with the library-backed handler method (`ListSubscriptions` / `RestoreSubscriptionsForSession`). Preserve behavior. Update `cmd/statusline_test.go` if affected.

- [ ] **Step 4: `cmd/unregister.go` (~62-65)**

"Soft-delete all subscriptions for this session" — 2b added `SoftDeleteSubscriptionsForSession` (library-backed, `RevokePrefix`). If unregister still calls a raw `UPDATE subscriptions SET deleted_at`, replace it with `d.SoftDeleteSubscriptionsForSession(sessionID)`. (Verify it isn't already using it — the survey showed a comment + error string at 62-65; read the actual call.) Update `cmd/unregister_test.go`.

- [ ] **Step 5: `cmd/user_prompt_submit.go` (~179)**

"Migrate subscriptions from archived session with same name" — same pattern as statusline ~1156. Use the library-backed restore path. Preserve behavior. Update its test.

- [ ] **Step 6: `cmd/api/events.go` (~73) and `cmd/api/resources.go` (~47)**

`cmd/api/events.go:73`: `JOIN subscriptions sub ON er.resource_type = sub.resource_type AND er.resource_id = sub.resource_id`. Change the joined table `subscriptions` → `watcher_subscriptions`. VERIFY the columns referenced (`resource_type`, `resource_id`, and any `session_id`/`deleted_at` predicate) — NOTE `watcher_subscriptions` has NO `session_id` column (it uses `subscriber`). If the JOIN filters by `session_id`, rewrite the predicate to `subscriber = 'handler:session:' || ?` (or use `sub.subscriber = ?` with `handlerSubscriber(sessionID)` bound). Read the full query and adjust column references to the `watcher_subscriptions` shape (`subscriber`, `deleted_at`, `expires_at`).
`cmd/api/resources.go:47`: `FROM subscriptions s` — same treatment; it likely lists a session's subscribed resources. Rewrite over `watcher_subscriptions` with `subscriber` = the handler subscriber string and the live-lease predicate `(s.expires_at IS NULL OR s.expires_at > ?)` + `s.deleted_at IS NULL`.

Update `cmd/api` tests accordingly (seed via the library).

- [ ] **Step 7: Confirm no legacy `subscriptions` SQL remains outside migration/inbox-legacy-arm/db-layer**

Run:
```bash
grep -rn --include="*.go" "\bsubscriptions\b" cmd/ db/ | grep -v "_test.go\|migrate_watcher.go\|legacy_guard.go\|watcher_subscriptions\|RestoreSubscriptions\|RenewSubscriptions\|SoftDeleteSubscriptions\|SubscribeIfNew\|ListSubscriptions\|// "
```
Expected: the only remaining raw `subscriptions` reference is in `db/inbox_scope.go` (the legacy inbox arm, removed in Task 5) and `db/migrate_watcher.go` + `db/legacy_guard.go`. Everything in `cmd/` is gone.

- [ ] **Step 8: Verify + commit**

Run: `go build ./... && go vet ./... && go test ./...` → all PASS.
```bash
git add cmd/statusline.go cmd/watching.go cmd/health.go cmd/unregister.go cmd/user_prompt_submit.go cmd/api/events.go cmd/api/resources.go <touched _test.go files>
git commit -s -m "refactor(cmd): read subscriptions from watcher library, drop direct legacy reads"
```

---

### Task 5: Handler — remove the inbox gate and the legacy single-query arm

**Files:**
- Modify: `db/inbox_scope.go` (remove the legacy branch of every helper; the UNION becomes unconditional)
- Modify: `db/events.go` (remove `gated := db.watcherMigrationDone()`; call the now-unconditional UNION helpers) — call sites at ~188, ~216, ~264, ~307, ~365
- Modify: `db/cursors.go` (~170 — same)
- Test: `db/events_test.go`, `db/inbox_union_test.go`, `db/cursors_test.go`

**Interfaces:**
- `db/inbox_scope.go` helpers currently take a `gated bool` and branch legacy-vs-UNION: `inboxSelect(selectKw string, cols inboxCols, gated bool) string`, `inboxSelectPred(selectKw, cols, gated, extraPred) string`, `inboxResourcesSelect(gated bool) string`, `inboxArgs(session *Session, cursor string, gated bool) []interface{}`. After this task they take NO `gated` param and always emit the UNION form.
- The `inboxCols` struct currently has a `legacy` field and a UNION field; remove the `legacy` field.

- [ ] **Step 1: Establish the current inbox tests pass (baseline)**

Run: `cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup && go test ./db/ -run 'Inbox|Unread|Cursor|Dismiss' -v`
Expected: PASS. These are the behaviors to PRESERVE. Note: existing tests may set the marker (`SetWatcherMigrated`) to exercise the UNION path — after this task the UNION is unconditional, so those tests should still pass WITHOUT needing to set the marker (but leaving the call in is harmless until Task 6 removes the setter).

- [ ] **Step 2: Make the UNION helpers unconditional in `db/inbox_scope.go`**

For each helper (`inboxSelect`, `inboxSelectPred`, `inboxResourcesSelect`, `inboxArgs`), delete the `if !gated { ...legacy... }` branch and the `gated` parameter, keeping only the UNION body. Remove the `legacy` field from `inboxCols` and its initializers (`inboxEventCols`, `inboxTypeCountCols`, `inboxCountCols`). Delete the now-unused legacy fragments (`inboxJoinSQL`, `inboxWhereSQL`, and any `legacy`-only constants). Delete the `watcherMigrationDone()` method and its exported wrapper `WatcherMigrationDone()` ONLY IF Task 6 no longer needs them — DEFER that deletion to Task 6 (the migration still sets the marker until Task 6 restructures it). For now, just stop *reading* the marker in the query path.

- [ ] **Step 3: Update `db/events.go` and `db/cursors.go` call sites**

At each of the 5 sites in `events.go` (~188, 216, 264, 307, 365) and 1 in `cursors.go` (~170): remove the `gated := db.watcherMigrationDone()` line and drop the `gated` argument from the `inboxSelect*`/`inboxArgs`/`inboxResourcesSelect` calls. Example transform:

```go
// before
gated := db.watcherMigrationDone()
query := inboxSelect("SELECT DISTINCT", inboxEventCols, gated) + `...`
rows, err := db.conn.Query(query, inboxArgs(session, cursor, gated)...)
// after
query := inboxSelect("SELECT DISTINCT", inboxEventCols) + `...`
rows, err := db.conn.Query(query, inboxArgs(session, cursor)...)
```

- [ ] **Step 4: Update tests**

In `db/events_test.go`/`db/inbox_union_test.go`/`db/cursors_test.go`, remove any `SetWatcherMigrated()` calls that were only there to force the UNION path (the UNION is now the only path). Any test that asserted *legacy-path* behavior (marker unset → single-query) must be deleted or converted to assert the UNION behavior. Keep the dismissal-on-both-arms test.

- [ ] **Step 5: Confirm the legacy arm is gone**

Run:
```bash
grep -rn --include="*.go" "watcherMigrationDone\|inboxJoinSQL\|inboxWhereSQL\|\.legacy\b\|gated" db/
```
Expected: `watcherMigrationDone`/`WatcherMigrationDone` may still appear (deferred to Task 6); `inboxJoinSQL`/`inboxWhereSQL`/`inboxCols.legacy`/`gated` params should be GONE.

- [ ] **Step 6: Verify + commit**

Run: `go build ./... && go vet ./... && go test ./...` → all PASS.
```bash
git add db/inbox_scope.go db/events.go db/cursors.go db/events_test.go db/inbox_union_test.go db/cursors_test.go
git commit -s -m "refactor(db): UNION inbox path is unconditional; remove legacy single-query arm"
```

---

### Task 6: Handler — restructure the migration lifecycle; schema-based legacy detection; `runMigrations` no-op

**Files:**
- Modify: `cmd/migrate_watcher.go` (absorb legacy migration steps; drop legacy tables at end; idempotent-on-absence)
- Modify: `db/db.go` (`runMigrations` → documented no-op that keeps CURRENT tables `dismissed_events`+`handler_meta`; remove legacy-only steps; `Open` no longer creates legacy tables)
- Modify: `db/legacy_guard.go` (`HasUnmigratedLegacyData` → schema-based)
- Modify: `db/inbox_scope.go` (delete `watcherMigrationDone`/`WatcherMigrationDone`/`setWatcherMigrated`/`SetWatcherMigrated` if no consumer remains — verify)
- Test: `cmd/migrate_watcher_test.go`, `db/legacy_guard_test.go` (or wherever `HasUnmigratedLegacyData` is tested), `cmd/root_test.go`

**Interfaces:**
- `HasUnmigratedLegacyData()` — SIGNATURE UNCHANGED (`func (db *DB) HasUnmigratedLegacyData() bool`), but implementation now checks `sqlite_master` for legacy table existence.
- The migration's marker-setting is removed; "migrated" is now signalled by legacy tables being ABSENT.

**IMPORTANT ordering/decision:** After Task 5, the ONLY consumer of the `watcher_migrated` marker is the migration itself (it sets it) and the setup/all-command guards (via `HasUnmigratedLegacyData`, which we're moving off the marker). So the marker can be fully retired. Confirm with a grep (Step 5) before deleting the setter/getter.

- [ ] **Step 1: Move legacy `runMigrations` steps into the migration; slim `runMigrations`**

Current `runMigrations` (`db/db.go`) does THREE things: (a) create `dismissed_events` (CURRENT — keep), (b) `addColumnIfMissing(subscriptions, unsubscribed_by, TEXT)` (LEGACY — move), (c) create `handler_meta` (was for the marker — since the marker is being retired, and no other handler_meta keys exist, DELETE `handler_meta` creation from the normal path; but VERIFY no other handler_meta usage first via grep). Keep (a). Move (b) into the migration command. Reduce `runMigrations` to:

```go
// runMigrations applies handler-owned schema migrations to the NEW schema on
// every Open. It is intentionally minimal: the legacy watcher tables and their
// historical migrations were removed in Phase 2c (handled once by
// `handler setup --migrate-watcher`). Add future new-schema migrations here as
// schema changes land.
func runMigrations(conn *sql.DB) error {
	// dismissed_events: per-session explicit event dismissals (current schema).
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS dismissed_events (
			session_id   TEXT NOT NULL,
			event_id     TEXT NOT NULL,
			dismissed_at TEXT NOT NULL,
			PRIMARY KEY (session_id, event_id)
		);
		CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
	`); err != nil {
		return fmt.Errorf("failed to create dismissed_events table: %w", err)
	}
	return nil
}
```

> DECISION on `dismissed_events`: it's current, but it's ALSO in `schema.sql` (line 70). `CREATE TABLE IF NOT EXISTS` in both is harmless/idempotent, matching the existing pattern (the original comment said runMigrations is a "safety net for existing databases created before a table was added to schema.sql"). Keep it in both. If the plan's reviewer prefers a single home, that's a separate cleanup — leave as-is here.
> If the grep in Step 5 shows `handler_meta` has NO remaining consumer, remove its creation here AND from `schema.sql` if present (it's created in `db/db.go` today, not schema.sql — verify). If ANY consumer remains, keep `handler_meta` creation in `runMigrations`.

- [ ] **Step 2: Make `--migrate-watcher` own the legacy lifecycle + idempotent-on-absence**

In `cmd/migrate_watcher.go`, `runMigrateWatcherAt` (or `migrateWatcherData`):
1. At the START, detect whether legacy tables exist (schema check — reuse the same helper as `HasUnmigratedLegacyData`, or a local `legacyTablesExist(conn) bool`). If NONE of the four legacy tables exist → print `"watcher data already migrated (nothing to do)"` and return nil. This replaces the old marker-based `WatcherMigrationDone()` guard.
2. Before copying, run the legacy schema migration that used to be in `runMigrations`: `addColumnIfMissing(conn, "subscriptions", "unsubscribed_by", "TEXT")` — this brings a legacy DB's `subscriptions` table current before reading `unsubscribed_by` in the copy. (Move the `addColumnIfMissing` helper or call it; it lives in `db/db.go` and is package `db` — expose it or replicate. Prefer: add an exported `db.EnsureLegacySubscriptionsColumn(conn)` in `db/legacy_guard.go` that the migration calls, keeping legacy knowledge centralized.)
3. Do the existing copy (events/event_resources/resource_state/resource_relationships/watcher_status/subscriptions → watcher_* tables) INSIDE the transaction, unchanged.
4. At the END of the transaction (after the copy, before/with commit), DROP the four legacy tables: `DROP TABLE IF EXISTS subscriptions; DROP TABLE IF EXISTS resource_state; DROP TABLE IF EXISTS resource_relationships; DROP TABLE IF EXISTS watcher_status;`. (Also drop their indexes — `DROP TABLE` drops associated indexes automatically in SQLite.)
5. REMOVE the `handler_meta watcher_migrated=1` marker write (no longer the signal). If `handler_meta` is being kept for other reasons, leave the table; just don't write the marker.

- [ ] **Step 3: Rewrite `HasUnmigratedLegacyData` schema-based**

In `db/legacy_guard.go`:

```go
// legacyDataTables are the pre-watcher-library handler tables. Their PRESENCE in
// the schema means this database predates the watcher-library migration and must
// be migrated (or discarded) before handler will work correctly. They are
// created only on pre-2c databases; fresh installs never create them, and
// `handler setup --migrate-watcher` drops them after copying their data into the
// library's watcher_* tables.
var legacyDataTables = []string{
	"subscriptions",
	"resource_state",
	"resource_relationships",
	"watcher_status",
}

// HasUnmigratedLegacyData reports whether this database still has any legacy
// watcher table in its schema. Detection is purely structural: if none of the
// legacy tables exist, the database is either a fresh install or already
// migrated. (github/jira rows may still linger in `events` post-migration until
// purged, but that is not a "must migrate" signal — table presence is.)
func (db *DB) HasUnmigratedLegacyData() bool {
	return anyLegacyTableExists(db.conn)
}

func anyLegacyTableExists(conn *sql.DB) bool {
	for _, table := range legacyDataTables {
		var name string
		err := conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table,
		).Scan(&name)
		if err == nil {
			return true
		}
	}
	return false
}
```

> DECISION: the old implementation also treated "github/jira events present" as legacy. Under the new schema-based detection, a *migrated* DB still has github/jira rows in `events` (until Task 7 purges them) but the legacy TABLES are gone — so it must NOT be flagged. Dropping the events-based signal is CORRECT for the new model. (A fresh install has neither legacy tables nor github/jira events, so it's also correctly false.)

- [ ] **Step 4: Fresh-install marker/guard interaction**

Since detection is now schema-based and fresh installs don't create legacy tables (Task 7 removes them from `schema.sql`), a fresh install naturally reports `HasUnmigratedLegacyData() == false` — no marker needed. Confirm the setup flow does NOT depend on the marker anymore. (The inbox gate that needed it is gone as of Task 5.)

- [ ] **Step 5: Retire the marker if unused**

Run:
```bash
grep -rn --include="*.go" "watcher_migrated\|watcherMigrationDone\|WatcherMigrationDone\|setWatcherMigrated\|SetWatcherMigrated\|handler_meta" cmd/ db/ | grep -v "_test.go"
```
If the ONLY remaining references are the definitions themselves (no callers), DELETE `watcherMigrationDone`/`WatcherMigrationDone`/`setWatcherMigrated`/`SetWatcherMigrated` from `db/inbox_scope.go`. Decide on `handler_meta`: if it has no other keys/consumers, remove its creation (from `runMigrations` and/or `schema.sql`) and stop creating it. If unsure, KEEP the empty `handler_meta` table (harmless) and note it for a future cleanup — do not block on it.

- [ ] **Step 6: Update tests (temp DBs only)**

- `cmd/migrate_watcher_test.go`: the existing tests seed legacy tables (via `db.Open` which currently creates them). After Task 7 removes legacy tables from `schema.sql`, tests must CREATE the legacy tables themselves before seeding (add a test helper `createLegacyTables(t, conn)` that runs the legacy DDL). Update the double-run test: after migrating once, the legacy tables are DROPPED, so a second `--migrate-watcher` run reports "already migrated (nothing to do)" via the schema check (not the marker). Assert the four legacy tables no longer exist post-migration (`sqlite_master` query). Assert the copy still lands in `watcher_*` correctly.
- `db/legacy_guard_test.go` / `cmd/root_test.go` (`TestLegacyUnmigrated`): update to reflect schema-based detection — fresh DB (no legacy tables) → false; DB with a legacy table created → true; after dropping the legacy tables → false. Remove the marker-based transitions.

> NOTE: because `db.Open` will no longer create legacy tables after Task 7, and Task 6's migration tests need them, add the `createLegacyTables` helper in THIS task so Task 6 tests are self-contained. The helper runs the exact legacy DDL (copy the four `CREATE TABLE` blocks from the pre-2c `schema.sql`).

- [ ] **Step 7: Verify + commit**

Run: `go build ./... && go vet ./... && go test ./...` → all PASS.
```bash
git add cmd/migrate_watcher.go db/db.go db/legacy_guard.go db/inbox_scope.go cmd/migrate_watcher_test.go db/legacy_guard_test.go cmd/root_test.go
git commit -s -m "refactor: migration owns legacy lifecycle; schema-based legacy detection; runMigrations no-op"
```

---

### Task 7: Handler — remove legacy tables from fresh schema; delete `watcher/` remnants; purge migrated `events` rows; pin v0.2.3; docs

This task has the one deliberately destructive step (the `events` purge). It is LAST and gated behind everything above being green.

**Files:**
- Modify: `db/schema.sql` (remove the four legacy `CREATE TABLE` blocks + their indexes)
- Delete: `cmd/watcher/` remnants that are dead post-2b — VERIFY each before deleting; `watcher/scheduler.go` STAYS (launchd/cron).
- Modify: `cmd/migrate_watcher.go` (add the `events` purge as an explicit, backed-up step — or a distinct sub-step of the migration AFTER the copy commits)
- Modify: `go.mod`, `go.sum` (drop local replace, pin v0.2.3)
- Modify: `README.md`, `docs/watcher-migration-runbook.md`
- Test: `cmd/migrate_watcher_test.go`, plus a fresh-install schema test

**Interfaces:** none new.

- [ ] **Step 1: Remove legacy tables from `db/schema.sql`**

Delete the `CREATE TABLE IF NOT EXISTS subscriptions (...)` + `idx_subscriptions_resource`, `CREATE TABLE IF NOT EXISTS watcher_status (...)`, `CREATE TABLE IF NOT EXISTS resource_relationships (...)`, and `CREATE TABLE IF NOT EXISTS resource_state (...)` blocks (schema.sql lines ~80-122). Keep everything else (events, event_recipients, event_resources, sessions, session_cursors, dismissed_events, cost_*, peek_state, daily_cost).

- [ ] **Step 2: Fresh-install schema test**

Add a test (e.g. `db/schema_test.go`) that opens a fresh temp DB via `db.Open` and asserts NONE of the four legacy tables exist (`sqlite_master`), while the current tables (events, sessions, watcher_* via `wdb.Migrate`) DO exist. This locks in "fresh installs never create legacy tables."

- [ ] **Step 3: Delete dead `watcher/` package files**

Run:
```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
ls cmd/watcher/ 2>/dev/null; ls watcher/ 2>/dev/null
grep -rn --include="*.go" "agent-handler/watcher\"" cmd/ db/ | grep -v scheduler
```
Identify files in handler's `watcher/` package that are no longer imported (2b deleted the in-tree pollers; confirm what remains). `watcher/scheduler.go` (and its `IsRunning`/`IsInstalled`, used by the migration guard and `cmd/watching.go`) STAYS. Delete only genuinely-unreferenced files. If everything except `scheduler.go` is already gone, this step is a no-op — confirm and move on.

- [ ] **Step 4: Add the `events` purge (destructive, backed-up, LAST)**

The migration already backs up the DB file at the start (`backupDBFile`). Add a purge of migrated rows from the legacy `events` table AFTER the copy transaction has committed successfully. Since the copy already inserted github/jira events into `watcher_events`, delete them from `events`:

```go
// After the copy commits: purge the migrated github/jira events from the legacy
// events table (they now live in watcher_events). event_resources rows for those
// events are removed too. Runs post-commit so a copy failure never deletes
// source data; the pre-migration backup covers rollback.
func purgeMigratedEvents(conn *sql.DB) (int64, error) {
	if _, err := conn.Exec(`
		DELETE FROM event_resources
		WHERE event_id IN (SELECT id FROM events WHERE source IN ('github','jira'))
	`); err != nil {
		return 0, fmt.Errorf("failed to purge migrated event_resources: %w", err)
	}
	res, err := conn.Exec(`DELETE FROM events WHERE source IN ('github','jira')`)
	if err != nil {
		return 0, fmt.Errorf("failed to purge migrated events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
```

Call `purgeMigratedEvents(d.Conn())` from `runMigrateWatcherAt` AFTER `migrateWatcherData` returns success, and print `"Purged N migrated github/jira rows from the legacy events table."`. Wrap it best-effort with a clear message if it errors (the copy already succeeded; a purge failure should warn, not fail the whole migration — the rows are harmless duplicates).

> Ordering note: the legacy-table DROPs (Task 6, inside the copy tx) and this events purge (post-commit) are distinct — `events` is NOT a dropped legacy table (it's a current table shared with agent/handler events), only its github/jira ROWS are purged.

- [ ] **Step 5: Update migration tests for the purge**

In `cmd/migrate_watcher_test.go`, after a successful migrate, assert `SELECT COUNT(*) FROM events WHERE source IN ('github','jira')` == 0 and that agent/handler-sourced events REMAIN. Assert `watcher_events` still has the copied rows.

- [ ] **Step 6: Drop the local replace, pin v0.2.3**

```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
go mod edit -dropreplace github.com/mturley/watcher
go get github.com/mturley/watcher@v0.2.3
go mod tidy
grep -n "mturley/watcher" go.mod   # expect: require ... v0.2.3, no replace
```

- [ ] **Step 7: Update docs**

- `docs/watcher-migration-runbook.md`: note that the migration now DROPS the legacy tables and PURGES migrated `events` rows (so rollback = restore the backup; the old binary + old tables come back together). Update the "old tables retained" language (they are no longer retained post-migration — the backup is the only rollback).
- `README.md`: the "Migrating an existing database" section — update to say the migration is now a one-way structural cleanup (with backup) rather than a copy-and-retain. Verify no README claim says legacy tables are retained.

- [ ] **Step 8: Full verify (cold) + commit**

Run:
```bash
cd /Users/mturley/.worktrees/agent-ledger/phase2c-cleanup
go build ./... && go vet ./... && go clean -testcache && go test ./...
```
Expected: all PASS against the published v0.2.3.
```bash
git add db/schema.sql db/schema_test.go cmd/migrate_watcher.go cmd/migrate_watcher_test.go go.mod go.sum README.md docs/watcher-migration-runbook.md <deleted watcher files>
git commit -s -m "feat: fresh installs skip legacy tables; migration drops legacy tables + purges events; pin watcher v0.2.3"
```

- [ ] **Step 9: STOP — human-gated integration**

Do NOT merge or tag a handler release. Report completion; the final whole-branch review + `finishing-a-development-branch` (merge decision) are human-gated. The production re-run of `--migrate-watcher` is NOT needed (the user already migrated in 2b; this branch changes fresh-install + migration-command behavior for OTHER/future users and for code cleanliness — the user's already-migrated DB has no legacy tables, so `HasUnmigratedLegacyData` is already false for them and the new code is a no-op on their DB).

> IMPORTANT for the user's already-migrated DB: after 2b's migration, the user's DB STILL HAS the legacy tables (2b copied, didn't drop) but the marker is SET. Under Task 6's schema-based detection, `HasUnmigratedLegacyData` will return TRUE for the user (legacy tables still present!), which would BLOCK all their commands after they upgrade to this branch. THIS IS A CRITICAL MIGRATION-PATH ISSUE — see "Global Constraints / user's DB state" below and handle it in Task 6.

---

## CRITICAL cross-cutting issue: the user's already-migrated DB (2b-migrated, tables retained)

The user ran 2b's `--migrate-watcher`, which **copied** data into `watcher_*` and **set the `watcher_migrated` marker** but **retained** the legacy tables. Under Phase 2c's new **schema-based** detection, "legacy tables exist" ⇒ `HasUnmigratedLegacyData() == true` ⇒ every command blocked. So simply shipping 2c would LOCK OUT the user (and anyone else who ran 2b).

**This must be handled in Task 6.** The detection or migration must account for "already-migrated (marker set) but legacy tables still present." Options for the implementer to resolve (surface to the human):
- (A) `HasUnmigratedLegacyData` returns false if the marker is set OR legacy tables are absent — i.e. keep reading the marker as an "already done" override during the transition, even while preferring schema-based detection. (Retains the marker; contradicts "retire the marker.")
- (B) The migration command, when it detects "marker set AND legacy tables present" (a 2b-migrated DB), SKIPS the copy and just DROPS the legacy tables (+ purges events) — a "finish the 2b migration" path. Then re-running `--migrate-watcher` once on such a DB completes the structural cleanup, after which schema-based detection is correct and the marker can be ignored/retired.
- (C) A tiny one-shot: on `Open`, if marker set AND legacy tables present, treat as migrated (don't block) — but that reintroduces marker-reading.

**Recommended: (B).** It keeps detection purely schema-based for the NORMAL case, and treats a 2b-migrated DB as "migration half-done — run it again to finish." The user (and any 2b adopter) runs `handler setup --migrate-watcher` once more; it detects marker-set-but-tables-present, skips the copy (data already in watcher_*), drops the legacy tables + purges events, done. Task 6 Step 2's "detect legacy tables" logic must therefore also check the marker to decide copy-vs-skip:
- legacy tables absent → "already migrated, nothing to do."
- legacy tables present + marker set → "finishing a prior migration" → skip copy, drop tables + purge.
- legacy tables present + marker unset → full migration (copy + drop + purge).

**This resolves the lockout**: after upgrading to 2c, the user's commands are blocked with the clear "run --migrate-watcher" message; they run it once; it finishes the cleanup; commands work. The runbook (Task 7 Step 7) must document this "second migration to finish 2c" for 2b adopters.

---

## Self-Review

**Spec coverage** (against the confirmed 2c design decisions):
- Retain migration code, centralized ✓ (Tasks 6/7 keep `--migrate-watcher`; legacy knowledge centralized in `legacy_guard.go` + `migrate_watcher.go`).
- Migration owns full lifecycle + idempotent + drops tables ✓ (Task 6 Step 2, Task 7 Step 4).
- `runMigrations` → no-op with comment ✓ (Task 6 Step 1).
- Fresh installs never create legacy tables ✓ (Task 7 Steps 1-2).
- Schema-based `HasUnmigratedLegacyData`, retire marker ✓ (Task 6 Steps 3, 5) — with the 2b-migrated-DB caveat handled (Option B).
- resource_relationships → library read API (v0.2.3) then move ✓ (Tasks 1-2).
- Remaining resource_state/subscriptions reads repointed ✓ (Tasks 3-4).
- Legacy inbox arm removed ✓ (Task 5).
- events purge LAST ✓ (Task 7 Step 4).
- No legacy backwards-compat clutter outside migration ✓ (grep gates in Tasks 2/3/4/5).

**Placeholder scan:** every code step has concrete code or exact grep/verify commands. The per-site sweep in Task 4 is specified per file with the transform to apply (not "handle the rest").

**Type consistency:** `SiblingResources(conn, watcher.Resource) ([]watcher.Resource, error)` used identically in Task 1 (def) and Task 2 (call). `wdb.GetResourceState`/`wdb.LinkResources` signatures match the library. `HasUnmigratedLegacyData()` signature unchanged across Tasks 6/7.

**Known risk flagged for the executor:** the 2b-migrated-DB lockout (the CRITICAL section) — the reviewer must confirm Task 6 implements Option B (or an approved alternative) or the user is locked out on upgrade.

---

## Execution Handoff

Execute **subagent-driven** (superpowers:subagent-driven-development) in the worktree `/Users/mturley/.worktrees/agent-ledger/phase2c-cleanup`. Per-task review after each; whole-branch review at the end; merge is human-gated.
