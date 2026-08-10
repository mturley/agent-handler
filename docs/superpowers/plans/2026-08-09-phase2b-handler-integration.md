# Phase 2b: agent-handler ⇄ watcher Library Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace agent-handler's in-tree `watcher/` package and its `subscriptions` table with the external `github.com/mturley/watcher@v0.2.0` library, including a one-time production data-migration command — while keeping the old tables and a legacy query path as a rollback backup (Phase 2c deletes them later).

**Architecture:** Handler adds the library as a dependency and calls its free functions with `d.Conn()` (handler's `*db.DB` already exposes `Conn() *sql.DB`). The library owns `watcher_*` tables (created by `watcher_db.Migrate`) alongside handler's own `runMigrations()`; the two are disjoint. Subscription storage moves to `watcher_subscriptions` keyed by an opaque `handler:session:<id>` subscriber string (no FK to `sessions`). Inbox reads become a UNION of an agent arm (handler's `events`) and a watcher arm (`watcher_events`), gated on a schema-version marker so the installed binary keeps working until migration runs.

**Tech Stack:** Go 1.25, `modernc.org/sqlite`, `github.com/mturley/watcher@v0.2.0` (dev via local `replace`), Cobra CLI, `database/sql`.

## Global Constraints

- Handler repo: `/Users/mturley/git/agent-ledger`, module `github.com/mturley/agent-handler`, go 1.25.0.
- Library: `github.com/mturley/watcher@v0.2.0`. During development use a `go.mod` `replace github.com/mturley/watcher => /Users/mturley/git/watcher`. Pin to `v0.2.0` and remove the `replace` only in the final task.
- The library packages are: `github.com/mturley/watcher` (root: `Resource`, `Event`, `EventType`, `Loop`), `github.com/mturley/watcher/db` (all the `watcher_*` DB functions), `github.com/mturley/watcher/config`, `github.com/mturley/watcher/github`, `github.com/mturley/watcher/jira`. Import the library's db package with an alias to avoid colliding with handler's own `db` package — use `wdb "github.com/mturley/watcher/db"` in handler files that also use handler's `db`.
- Library functions are FREE FUNCTIONS taking `*sql.DB`. Handler exposes the connection via `func (db *DB) Conn() *sql.DB` (db/db.go:130). Call e.g. `wdb.Subscribe(d.Conn(), subscriber, r, opts)`.
- Subscriber string format is EXACTLY `"handler:session:" + sessionID`. Define one helper `handlerSubscriber(sessionID string) string` and one parser `sessionIDFromSubscriber(subscriber string) (string, bool)` and use them everywhere — never inline the format.
- The library's `watcher.Resource` has fields `Type, ID, URL` (all string). Handler's resource identifiers today are `(resourceType, resourceID, resourceURL)` — map directly.
- `watcher_*` tables belong to the library's `Migrate`; handler's `runMigrations()` must NEVER create/alter a `watcher_*` table. Keep them disjoint.
- Reads are gated on the `watcher_schema_version` marker (via `wdb.SchemaVersion(conn) >= 1`), NEVER on row counts. Before migration → legacy single-table path; after → UNION path.
- Dismissal (`dismissed_events`, bare `event_id`) must be applied to BOTH UNION arms. Relies on `event_id` uniqueness across `events` and `watcher_events` (both UUIDs).
- The `source` allowlist for migrating events is EXACTLY `('github','jira')`. Handler's own `agent` and `handler` source events STAY in `events`.
- 5-day lease TTL: define `const sessionLeaseTTL = 5 * 24 * time.Hour` once and reuse.
- Phase 2b RETAINS the old tables (`subscriptions`, `resource_state`, `resource_relationships`, `watcher_status`) and the legacy query path. Do NOT delete them — that is Phase 2c.
- Handler keeps its existing launchd/cron scheduling invoking `handler watcher run <name>`. Do NOT migrate to the library's `scheduler` package in 2b.
- Adding the library bumps `modernc.org/sqlite` (handler v1.52.0 → library v1.56.0). Run `go mod tidy` and the full suite after adding the dep; treat any driver-behavior surprise as a task-blocking finding.
- NEVER run the production migration against the real `~/.agent-handler/handler.db` during development. All migration tests use temp databases. The real migration is run by the human, once, via the documented runbook.

---

## File Structure

New files:
- `watcherbridge/subscriber.go` — `handlerSubscriber`/`sessionIDFromSubscriber` helpers + `sessionLeaseTTL`. (New small package so both `db/` and `cmd/` can import it without import cycles; if handler convention prefers, put in an existing leaf package — see Task 2.)
- `cmd/migrate_watcher.go` — the `handler setup --migrate-watcher` command and its migration logic.
- `db/watcher_bridge.go` — handler-side wrappers that call the library db functions with `d.Conn()` and translate handler types (keeps call sites in cmd/ readable and gives the subscription-replacement one home).

Modified files (major):
- `go.mod` / `go.sum` — add dependency + replace.
- `db/db.go` — call `wdb.Migrate(conn)` in `Open()` after `runMigrations()`.
- `db/subscriptions.go` — reimplement the 8 methods over the library (or delegate from `db/watcher_bridge.go`).
- `db/inbox_scope.go` — UNION rewrite, gated.
- `db/events.go` (lines 190, 221, 273, 323, 385) and `db/cursors.go` (line 156) — the 6 callers that compose the inbox fragments.
- `db/resources.go` — `SessionsForResource`, `FindRelatedSessions`, `ResourceHistory` over the library.
- `db/watcher_status.go` — DELETE (readers repoint to library).
- `cmd/statusline.go`, `cmd/watching.go`, `cmd/triage.go`, `cmd/status.go`, `cmd/api/resources.go` — repoint watcher_status reads.
- `cmd/register.go`, `cmd/statusline.go`, `cmd/unregister.go`, `cmd/user_prompt_submit.go`, `cmd/cleanup.go` — lease wiring.
- `cmd/watcher/*.go` — thin wrappers over library pollers; credentials via `auth.yaml`.
- cursor-advance sites — `max(ts)` fix.

Task ordering rationale: dependency + Migrate first (Task 1–2), then the read/write layers behind the gate against a TEST db (Tasks 3–8), then the poller wrappers (Task 9), then the migration command last and most carefully (Task 10), then pin + release (Task 11). The production DB is never touched until the human runs the runbook.

---

### Task 1: Add the library dependency (replace directive) and wire `Migrate`

**Files:**
- Modify: `go.mod`, `go.sum`, `db/db.go`
- Test: `db/watcher_migrate_test.go` (new)

**Interfaces:**
- Produces: after `db.Open`, the connection has the `watcher_*` tables. `wdb.SchemaVersion(d.Conn())` returns `>= 1`.

- [ ] **Step 1: Add the replace + require**

Run:
```bash
cd /Users/mturley/git/agent-ledger
go mod edit -replace github.com/mturley/watcher=/Users/mturley/git/watcher
go get github.com/mturley/watcher@v0.2.0
go mod tidy
```
Expected: `go.mod` gains `require github.com/mturley/watcher v0.2.0` and the `replace` line; `modernc.org/sqlite` bumps to v1.56.0.

- [ ] **Step 2: Write the failing test**

Create `db/watcher_migrate_test.go`:
```go
package db

import (
	"path/filepath"
	"testing"

	wdb "github.com/mturley/watcher/db"
)

func TestOpenRunsWatcherMigrate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	v, err := wdb.SchemaVersion(d.Conn())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v < 1 {
		t.Fatalf("watcher schema not migrated: version %d", v)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./db/ -run TestOpenRunsWatcherMigrate -v`
Expected: FAIL (watcher schema version 0 — Migrate not called yet).

- [ ] **Step 4: Call `wdb.Migrate` in `Open`**

In `db/db.go` `Open()`, after the `runMigrations(conn)` block and before `return &DB{conn: conn}, nil`, add:
```go
	// Create the watcher library's own tables (watcher_*). Disjoint from
	// handler's runMigrations above, which never touches watcher_* tables.
	if err := wdb.Migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run watcher migrate: %w", err)
	}
```
Add the import `wdb "github.com/mturley/watcher/db"` to `db/db.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./db/ -v` then `go build ./... && go vet ./... && go test ./...`
Expected: all PASS (the new test + entire existing suite; confirms the sqlite bump didn't regress anything).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum db/db.go db/watcher_migrate_test.go
git commit -s -m "feat(db): add watcher v0.2.0 dependency and run its Migrate on Open"
```

---

### Task 2: Subscriber-string helpers

**Files:**
- Create: `db/watcher_bridge.go`
- Test: `db/watcher_bridge_test.go`

**Interfaces:**
- Produces (in handler's `db` package):
  - `const sessionLeaseTTL = 5 * 24 * time.Hour`
  - `func handlerSubscriber(sessionID string) string` → `"handler:session:" + sessionID`
  - `func handlerSubscriberPrefix() string` → `"handler:session:"`
  - `func sessionIDFromSubscriber(subscriber string) (string, bool)` → strips the prefix; `false` if it doesn't match.

Placing these in handler's `db` package (not a new package) avoids an import cycle and matches where the subscription code lives.

- [ ] **Step 1: Write the failing test**

Create `db/watcher_bridge_test.go`:
```go
package db

import "testing"

func TestSubscriberRoundTrip(t *testing.T) {
	s := handlerSubscriber("sess-abc")
	if s != "handler:session:sess-abc" {
		t.Fatalf("handlerSubscriber = %q", s)
	}
	id, ok := sessionIDFromSubscriber(s)
	if !ok || id != "sess-abc" {
		t.Fatalf("sessionIDFromSubscriber(%q) = %q,%v", s, id, ok)
	}
	if _, ok := sessionIDFromSubscriber("worktree:foo"); ok {
		t.Fatal("non-handler subscriber should not parse")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/ -run TestSubscriberRoundTrip -v`
Expected: FAIL (undefined helpers).

- [ ] **Step 3: Implement**

Create `db/watcher_bridge.go`:
```go
package db

import (
	"strings"
	"time"
)

// sessionLeaseTTL is how long a session's watcher subscription leases last
// before expiry. Renewed on the session heartbeat; long enough to survive a
// closed laptop over a weekend.
const sessionLeaseTTL = 5 * 24 * time.Hour

const subscriberPrefix = "handler:session:"

// handlerSubscriber is the opaque watcher-library subscriber string handler
// uses for a session. The session id is embedded and recovered by
// sessionIDFromSubscriber; the library never interprets it.
func handlerSubscriber(sessionID string) string { return subscriberPrefix + sessionID }

// handlerSubscriberPrefix is the prefix matching all handler session
// subscriptions, for prefix renew/revoke/list operations.
func handlerSubscriberPrefix() string { return subscriberPrefix }

// sessionIDFromSubscriber recovers the session id from a handler subscriber
// string, or returns ("", false) if the string is not a handler subscriber.
func sessionIDFromSubscriber(subscriber string) (string, bool) {
	if !strings.HasPrefix(subscriber, subscriberPrefix) {
		return "", false
	}
	return strings.TrimPrefix(subscriber, subscriberPrefix), true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./db/ -run TestSubscriberRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/watcher_bridge.go db/watcher_bridge_test.go
git commit -s -m "feat(db): handler:session subscriber-string helpers + lease TTL"
```

---

### Task 3: Reimplement subscription write methods over the library

Reimplement handler's subscription WRITE methods (`Subscribe`, `SubscribeIfNew`, `Unsubscribe`, `Reinstate`, `SoftDeleteSubscriptionsForSession`, `SoftDeleteSubscriptionsForBranch`, `RestoreSubscriptionsForSession`) so they call the library against `watcher_subscriptions`, keyed by `handlerSubscriber(sessionID)`. Keep the SAME method signatures on `*db.DB` so existing callers compile unchanged.

**Files:**
- Modify: `db/subscriptions.go`
- Test: `db/subscriptions_test.go` (rewrite the assertions against the new backing store)

**Interfaces:**
- Consumes: Task 2 helpers; `wdb.Subscribe/UserUnsubscribe/Reinstate/RevokePrefix/AllSubscriptions/ActiveSubscriptions` (from `github.com/mturley/watcher/db`), `watcher.Resource`.
- Produces (unchanged signatures, new backing store):
  - `Subscribe(s Subscription) error` → `wdb.Subscribe(conn, handlerSubscriber(s.SessionID), watcher.Resource{Type:s.ResourceType,ID:s.ResourceID,URL:deref(s.ResourceURL)}, wdb.SubscribeOpts{TTL: sessionLeaseTTL})`
  - `SubscribeIfNew(s Subscription) error` → same but `SubscribeOpts{TTL: sessionLeaseTTL, IfAbsent: true}`
  - `Unsubscribe(sessionID, resourceType, resourceID string) error` → `wdb.UserUnsubscribe(conn, handlerSubscriber(sessionID), r)` (handler's Unsubscribe is the USER-initiated one — it previously set `unsubscribed_by='user'`).
  - `Reinstate(sessionID, resourceType, resourceID string) error` → `wdb.Reinstate(conn, handlerSubscriber(sessionID), r)`
  - `SoftDeleteSubscriptionsForSession(sessionID string) (int, error)` → `wdb.RevokePrefix(conn, handlerSubscriber(sessionID))` then return a count (see note).
  - `SoftDeleteSubscriptionsForBranch(branch string) (int, error)` → resolve the branch's session ids from `sessions`, `wdb.RevokePrefix` each; sum counts.
  - `RestoreSubscriptionsForSession(sessionID string) (int, error)` → COMPOSED: `wdb.AllSubscriptions(conn, handlerSubscriber(sessionID), true)`; for each row with `!UnsubscribedByUser && DeletedAt != nil`, call `wdb.Reinstate(conn, row.Subscriber, row.Resource)`; count reinstated.

Note on counts: `RevokePrefix`/`Reinstate` are void in the library. To preserve handler's `(int, error)` returns, query the affected count via `wdb.AllSubscriptions` before/after, OR change internal callers to ignore the count. SIMPLEST that preserves behavior: for `SoftDeleteSubscriptionsForSession`, count live subs via `wdb.ActiveSubscriptions(conn, sub, false)` BEFORE revoking, return that length. For `RestoreSubscriptionsForSession`, count the rows you reinstate in the loop.

- [ ] **Step 1: Rewrite the tests first (define expected behavior)**

In `db/subscriptions_test.go`, rewrite each test to assert via the new store. Example for the core round-trip (replace the existing equivalent):
```go
func TestSubscribeAndList(t *testing.T) {
	d := testDB(t) // existing helper that calls Open (now also runs watcher Migrate)
	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatal(err)
	}
	subs, err := d.ListSubscriptions("s1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].ResourceID != "o/r#1" {
		t.Fatalf("got %+v", subs)
	}
}

func TestUnsubscribeIsUserProtected(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	if err := d.Unsubscribe("s1", "pr", "o/r#1"); err != nil {
		t.Fatal(err)
	}
	// user-unsubscribe must NOT be revived by SubscribeIfNew
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 0 {
		t.Fatalf("user-unsubscribed resource must stay inactive, got %+v", subs)
	}
}

func TestRestoreSkipsUserUnsubscribed(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "keep"})
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "user-gone"})
	d.Unsubscribe("s1", "pr", "user-gone")          // user tombstone
	d.SoftDeleteSubscriptionsForSession("s1")        // session end (non-user revoke of the rest)
	n, err := d.RestoreSubscriptionsForSession("s1") // session return
	if err != nil {
		t.Fatal(err)
	}
	subs, _ := d.ListSubscriptions("s1", false)
	// "keep" comes back; "user-gone" stays gone
	if len(subs) != 1 || subs[0].ResourceID != "keep" {
		t.Fatalf("restore should revive only non-user subs, got %+v (n=%d)", subs, n)
	}
}
```
(`ListSubscriptions` is rewritten in Task 4; if it's not yet available when this task runs, assert directly via `wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)` instead, then tighten in Task 4. Prefer implementing Task 3 and Task 4 together if the reviewer finds the split awkward — they share the test file.)

- [ ] **Step 2: Run to verify fail**

Run: `go test ./db/ -run 'TestSubscribe|TestUnsubscribe|TestRestore' -v`
Expected: FAIL (methods still hit the old table / assertions mismatch).

- [ ] **Step 3: Reimplement the write methods**

Rewrite each method body in `db/subscriptions.go` to call the library per the Interfaces block above. Add a small `deref(*string) string` helper (empty string when nil) if not already present. Keep the `Subscription` struct and method signatures identical. Delete the old raw-SQL bodies that hit the `subscriptions` table. Example:
```go
func (db *DB) SubscribeIfNew(s Subscription) error {
	return wdb.Subscribe(db.conn, handlerSubscriber(s.SessionID),
		watcher.Resource{Type: s.ResourceType, ID: s.ResourceID, URL: deref(s.ResourceURL)},
		wdb.SubscribeOpts{TTL: sessionLeaseTTL, IfAbsent: true})
}

func (db *DB) Unsubscribe(sessionID, resourceType, resourceID string) error {
	return wdb.UserUnsubscribe(db.conn, handlerSubscriber(sessionID),
		watcher.Resource{Type: resourceType, ID: resourceID})
}

func (db *DB) RestoreSubscriptionsForSession(sessionID string) (int, error) {
	sub := handlerSubscriber(sessionID)
	all, err := wdb.AllSubscriptions(db.conn, sub, false)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range all {
		if row.UnsubscribedByUser || row.DeletedAt == nil {
			continue
		}
		if err := wdb.Reinstate(db.conn, sub, row.Resource); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
```
Import `watcher "github.com/mturley/watcher"` and `wdb "github.com/mturley/watcher/db"`.

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v`
Expected: PASS. Then `go build ./...` (confirms all existing callers of these methods still compile against the unchanged signatures).

- [ ] **Step 5: Commit**

```bash
git add db/subscriptions.go db/subscriptions_test.go
git commit -s -m "feat(db): reimplement subscription writes over watcher_subscriptions"
```

---

### Task 4: Reimplement subscription READ methods (`ListSubscriptions`, `db/resources.go` lookups)

**Files:**
- Modify: `db/subscriptions.go` (`ListSubscriptions`), `db/resources.go` (`SessionsForResource`, `FindRelatedSessions`, `ResourceHistory`)
- Test: `db/subscriptions_test.go`, `db/resources_test.go`

**Interfaces:**
- Consumes: Task 2 helpers; `wdb.ActiveSubscriptions/AllSubscriptions/SubscribersOf/EventsForResource`.
- Produces (unchanged signatures):
  - `ListSubscriptions(sessionID string, includeDeleted bool) ([]Subscription, error)` → `wdb.AllSubscriptions` when `includeDeleted`, else `wdb.ActiveSubscriptions`, over `handlerSubscriber(sessionID)` exact; map each `wdb.Subscription` → handler `Subscription`.
  - `SessionsForResource(resourceType, resourceID string) ([]Subscription, error)` → `wdb.SubscribersOf(conn, resourceType, resourceID)`; for each, `sessionIDFromSubscriber`; skip non-handler subscribers; build handler `Subscription`s.
  - `FindRelatedSessions(sessionID string) ([]Session, error)` → reimplement using `SubscribersOf` for the session's resources (see Step 3) + relationship data; return the same shape.
  - `ResourceHistory(resourceType, resourceID string, limit int) ([]Event, error)` → `wdb.EventsForResource(conn, resourceType, resourceID)`, map `watcher.Event` → handler `Event`, apply `limit`.

- [ ] **Step 1: Write/rewrite the failing tests**

Add to `db/subscriptions_test.go` a `ListSubscriptions` include-deleted test, and to `db/resources_test.go` a `SessionsForResource` test:
```go
func TestSessionsForResourceParsesSessionIDs(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	d.SubscribeIfNew(Subscription{SessionID: "s2", ResourceType: "pr", ResourceID: "o/r#1"})
	subs, err := d.SessionsForResource("pr", "o/r#1")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range subs {
		ids[s.SessionID] = true
	}
	if !ids["s1"] || !ids["s2"] || len(ids) != 2 {
		t.Fatalf("want s1,s2; got %v", ids)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./db/ -run 'TestList|TestSessionsForResource|TestResourceHistory|TestFindRelated' -v`
Expected: FAIL.

- [ ] **Step 3: Implement the read methods**

Rewrite each method to call the library and map types. For `ListSubscriptions`:
```go
func (db *DB) ListSubscriptions(sessionID string, includeDeleted bool) ([]Subscription, error) {
	sub := handlerSubscriber(sessionID)
	var rows []wdb.Subscription
	var err error
	if includeDeleted {
		rows, err = wdb.AllSubscriptions(db.conn, sub, false)
	} else {
		rows, err = wdb.ActiveSubscriptions(db.conn, sub, false)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, subFromWatcher(sessionID, r))
	}
	return out, nil
}
```
Add a `subFromWatcher(sessionID string, r wdb.Subscription) Subscription` mapper in `db/watcher_bridge.go` (maps Resource fields, DeletedAt, etc. into handler's `Subscription`). For `SessionsForResource`, iterate `wdb.SubscribersOf`, use `sessionIDFromSubscriber`, skip non-handler. For `FindRelatedSessions`, reimplement over `SubscribersOf` + handler's relationship reads. For `ResourceHistory`, map `wdb.EventsForResource` results and truncate to `limit`.

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v && go build ./...`
Expected: PASS and everything compiles.

- [ ] **Step 5: Commit**

```bash
git add db/subscriptions.go db/resources.go db/subscriptions_test.go db/resources_test.go db/watcher_bridge.go
git commit -s -m "feat(db): reimplement subscription/resource reads over the watcher library"
```

---

### Task 5: Lease wiring at the session lifecycle seams

**Files:**
- Modify: `cmd/register.go` (~:134), `cmd/statusline.go` (~:1047, ~:1103, ~:1154, + heartbeat), `cmd/unregister.go` (~:63), `cmd/user_prompt_submit.go` (~:202), `cmd/cleanup.go`
- Test: `cmd/cleanup_test.go` (new or extend) for the new revoke; the others are exercised via existing behavior + the db tests.

**Interfaces:**
- Consumes: the reimplemented `db.DB` methods (Tasks 3–4) — most seams already call `SubscribeIfNew`/`SoftDeleteSubscriptionsForSession`/`RestoreSubscriptionsForSession`, which now hit the library, so they mostly work unchanged. The NEW work is: (a) heartbeat renew, (b) cleanup revoke.
- Produces: a live session's leases are renewed on heartbeat; `handler cleanup` revokes archived sessions' subscriptions.

- [ ] **Step 1: Heartbeat renew — write the intent test (db-level)**

The heartbeat lives in `cmd/statusline.go`. Add a db method to renew a session's leases and test it:
In `db/subscriptions.go` add:
```go
func (db *DB) RenewSubscriptionsForSession(sessionID string) error {
	return wdb.Renew(db.conn, handlerSubscriber(sessionID), sessionLeaseTTL)
}
```
Wait — `wdb.Renew` is exact-subscriber and matches all of that subscriber's live rows, which is exactly one session. Good. Test in `db/subscriptions_test.go`:
```go
func TestRenewExtendsSessionLeases(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	if err := d.RenewSubscriptionsForSession("s1"); err != nil {
		t.Fatal(err)
	}
	// still active after renew
	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 1 {
		t.Fatalf("want 1 active after renew, got %d", len(subs))
	}
}
```

- [ ] **Step 2: Run to verify fail, then implement the method**

Run: `go test ./db/ -run TestRenewExtendsSessionLeases -v` (FAIL: undefined). Add the method (Step 1 code). Re-run: PASS.

- [ ] **Step 3: Call renew on the heartbeat**

In `cmd/statusline.go`, find the periodic heartbeat path (the same place that updates `last_active` / syncs session metadata each statusline render). After the session is confirmed active, add:
```go
	_ = d.RenewSubscriptionsForSession(input.SessionID)
```
(Best-effort; a failed renew must not break the statusline. The 5-day TTL means occasional misses are harmless.)

- [ ] **Step 4: Cleanup revoke — write the failing test**

`cmd/cleanup.go` archives dead/stale sessions but never revokes their subscriptions. Add revocation. Test (in `cmd/cleanup_test.go`, create if absent) that after cleanup archives a session, that session's subscriptions are revoked. Since cleanup logic is in `cmd`, test at the db seam it will call — add a db method and test it:
```go
// db/subscriptions.go
func (db *DB) RevokeSubscriptionsForSession(sessionID string) error {
	return wdb.RevokePrefix(db.conn, handlerSubscriber(sessionID))
}
```
```go
// db/subscriptions_test.go
func TestRevokeForSessionClearsLeases(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	if err := d.RevokeSubscriptionsForSession("s1"); err != nil {
		t.Fatal(err)
	}
	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 0 {
		t.Fatalf("want 0 active after revoke, got %d", len(subs))
	}
}
```
Note: `SoftDeleteSubscriptionsForSession` (used by unregister) already maps to `RevokePrefix` (Task 3), so unregister is covered. This new `RevokeSubscriptionsForSession` is for cleanup; it is effectively the same call — you MAY reuse `SoftDeleteSubscriptionsForSession` directly in cleanup instead of adding a new method. Prefer reusing it to avoid two names for one behavior; drop this method if so and call `d.SoftDeleteSubscriptionsForSession(id)` in cleanup.

- [ ] **Step 5: Wire cleanup**

In `cmd/cleanup.go`, in the loop that archives each session (where it currently calls `ArchiveSessions`/`DeletePeekStatesForSessions`), for each archived session id also call `d.SoftDeleteSubscriptionsForSession(id)` (ignore the count). This fixes the latent inconsistency where cleanup archived a session but left its subscriptions polling.

- [ ] **Step 6: Run tests + build**

Run: `go test ./... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add db/subscriptions.go db/subscriptions_test.go cmd/statusline.go cmd/cleanup.go cmd/cleanup_test.go
git commit -s -m "feat(cmd): renew leases on heartbeat, revoke subscriptions on cleanup"
```

---

### Task 6: Inbox UNION rewrite (gated), agent + watcher arms

This is the highest-risk task. `db/inbox_scope.go` currently exposes `inboxJoinSQL`, `inboxWhereSQL`, `inboxScopeArgs`, composed by 6 callers: `db/events.go` lines 190, 221, 273, 323, 385 and `db/cursors.go` line 156. Convert the composed query to a UNION of an agent arm (handler `events`) and a watcher arm (`watcher_events`), gated on the schema-version marker.

**Files:**
- Modify: `db/inbox_scope.go`, `db/events.go` (5 sites), `db/cursors.go` (1 site)
- Test: `db/inbox_union_test.go` (new)

**Interfaces:**
- Consumes: `wdb.SchemaVersion`; the `watcher_events`/`watcher_event_resources`/`watcher_subscriptions` tables; `dismissedExclusionSQL` (db/events.go), `inboxExcludedTypesSQL` (db/events.go).
- Produces: a new composed builder the 6 callers use:
  - `func inboxSelect(cols string) string` — returns the full `SELECT cols FROM ( <agent arm> UNION ALL <watcher arm> ) e` when the marker is set, or the legacy `SELECT cols <inboxJoinSQL> WHERE <inboxWhereSQL>` when not. Because column lists differ per caller, the builder takes the caller's projected columns.
  - `func inboxArgs(session *Session, cursor string, gated bool) []interface{}` — returns the arg slice for whichever path `inboxSelect` produced.
  - `func (db *DB) inboxGated() bool` — `wdb.SchemaVersion(db.conn) >= 1 && <marker set>`. Since `wdb.Migrate` always sets the version, gate on a SEPARATE handler-owned marker that the migration command sets (see Task 10) — e.g. a row in a `handler_meta` table or a dedicated `watcher_migration_done` flag. DEFINE this marker here: add `func (db *DB) watcherMigrationDone() bool` reading a `handler_meta(key,value)` row `watcher_migrated=1`; default false. (Do NOT gate on `wdb.SchemaVersion` alone — that's true immediately after Task 1, before data is migrated.)

CRITICAL: the gate must be the DATA-migration marker, not the schema presence. Task 1 makes the watcher tables exist immediately; reads must stay on the legacy path until Task 10's migration copies the data and sets `watcher_migrated=1`.

- [ ] **Step 1: Define the handler_meta marker + failing test**

Add to handler's `runMigrations` (db/db.go) a `CREATE TABLE IF NOT EXISTS handler_meta (key TEXT PRIMARY KEY, value TEXT)`. Add `db/inbox_scope.go` helpers `func (db *DB) watcherMigrationDone() bool` (SELECT value FROM handler_meta WHERE key='watcher_migrated'; true iff '1') and `func (db *DB) setWatcherMigrated() error`. Test:
```go
func TestWatcherMigrationMarkerDefaultsFalse(t *testing.T) {
	d := testDB(t)
	if d.watcherMigrationDone() {
		t.Fatal("marker should default false")
	}
	if err := d.setWatcherMigrated(); err != nil {
		t.Fatal(err)
	}
	if !d.watcherMigrationDone() {
		t.Fatal("marker should be true after set")
	}
}
```

- [ ] **Step 2: Run to verify fail, implement marker, pass**

Run: `go test ./db/ -run TestWatcherMigrationMarker -v` (FAIL). Implement `handler_meta` + the two methods. Re-run: PASS.

- [ ] **Step 3: Write the UNION behavior test**

Create `db/inbox_union_test.go`. Seed BOTH an agent event (via handler's InsertEvent to `events`, routed to a session by recipient) AND a watcher event (via `wdb.InsertEvent` to `watcher_events` for a resource the session subscribes to). Set the marker. Assert `UnreadForSession` returns BOTH, ordered by ts; assert a dismissed watcher event is excluded; assert with the marker UNSET only the agent event is returned (legacy path).
```go
func TestInboxUnionReturnsBothArms(t *testing.T) {
	d := testDB(t)
	sess := seedSession(t, d, "s1", "repo", "branch") // existing helper or inline UpsertSession
	// agent event addressed to s1
	insertAgentEventForSession(t, d, "s1", "2026-01-01T00:00:01Z")
	// watcher event for a resource s1 subscribes to
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	insertWatcherEvent(t, d, "pr", "o/r#1", "2026-01-01T00:00:02Z")
	d.setWatcherMigrated()

	events, err := d.UnreadForSession("s1")
	if err != nil { t.Fatal(err) }
	if len(events) != 2 {
		t.Fatalf("want 2 (agent+watcher), got %d", len(events))
	}
}
```
(Write the `insertWatcherEvent`/`insertAgentEventForSession` helpers using `wdb.InsertEvent` and handler's `InsertEvent` respectively.)

- [ ] **Step 4: Run to verify fail**

Run: `go test ./db/ -run TestInboxUnion -v`
Expected: FAIL (watcher arm not wired; only agent events returned).

- [ ] **Step 5: Rewrite inbox_scope.go**

Replace the fragment constants with the gated builder. The agent arm keeps handler's existing recipient/broadcast/branch/role routing against `events` (drop the `subscriptions`/`event_resources` join from the agent arm — routing to resources now lives in the watcher arm). The watcher arm:
```sql
SELECT <cols> FROM watcher_events e
JOIN watcher_event_resources er ON er.event_id = e.id
JOIN watcher_subscriptions s ON s.resource_type = er.resource_type AND s.resource_id = er.resource_id
WHERE s.subscriber = ? AND s.deleted_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > ?)
  AND e.ts > ?
  AND e.type NOT IN ('watch_started','watcher_error')
  AND NOT EXISTS (SELECT 1 FROM dismissed_events d WHERE d.session_id = ? AND d.event_id = e.id)
```
Both arms `UNION ALL`; both apply the dismissal NOT EXISTS. Provide `inboxSelect(cols)`/`inboxArgs(session,cursor,gated)`; when `!gated`, emit the legacy single query verbatim (keep the old constants for that path). The projected `cols` must be identical column names/types in both arms so the outer query can order/aggregate.

- [ ] **Step 6: Update the 6 callers**

At `db/events.go:190, 221, 273, 323, 385` and `db/cursors.go:156`, replace the `inboxJoinSQL + " WHERE " + inboxWhereSQL` composition + `inboxScopeArgs(...)` with `inboxSelect(<that caller's cols>)` + `inboxArgs(session, cursor, db.watcherMigrationDone())`. Each caller already knows its own SELECT/GROUP BY/ORDER BY — wrap accordingly. Preserve each caller's existing projected columns.

- [ ] **Step 7: Run tests**

Run: `go test ./db/ -v && go test ./... && go build ./...`
Expected: PASS — including the new union test AND all existing unread/inbox/cursor tests (they run with the marker unset → legacy path, so they must still pass unchanged).

- [ ] **Step 8: Commit**

```bash
git add db/inbox_scope.go db/events.go db/cursors.go db/db.go db/inbox_union_test.go
git commit -s -m "feat(db): gated inbox UNION (agent + watcher arms), dismissal on both"
```

---

### Task 7: Cursor-advance `max(ts)` fix

**Files:**
- Modify: `db/cursors.go` and any cursor-advance call sites that use `time.Now()`
- Test: `db/cursors_test.go`

**Interfaces:**
- Produces: cursor advances to the max `ts` of the events actually returned to the caller, not wall-clock now.

- [ ] **Step 1: Write the failing test**

Find handler's cursor-advance function (e.g. `AdvanceCursor`/`MarkRead`). Add a test that inserts two events at known ts, reads+advances, inserts a third with a ts BETWEEN the previous now and the read time (simulate same-second), and asserts it is NOT skipped. If the current API advances by `time.Now()`, this test fails.
```go
func TestCursorAdvancesToMaxEventTS(t *testing.T) {
	d := testDB(t)
	// ... seed session s1, two agent events at T1<T2 addressed to s1
	// read unread + advance
	// assert cursor == T2 (the max returned ts), not "now"
}
```
(Write it against handler's real cursor API — inspect `db/cursors.go` for the exact function names.)

- [ ] **Step 2: Run to verify fail**

Run: `go test ./db/ -run TestCursorAdvancesToMaxEventTS -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Change the advance path to take/compute the max `ts` of the returned event set and store that as the cursor, leaving the cursor unchanged when the set is empty. Update callers that pass `time.Now()`.

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add db/cursors.go db/cursors_test.go
git commit -s -m "fix(db): advance cursor to max(ts) of returned events, not now"
```

---

### Task 8: Repoint watcher_status readers to the library poller status

**Files:**
- Modify: `cmd/statusline.go`, `cmd/watching.go`, `cmd/triage.go`, `cmd/status.go`, `cmd/api/resources.go`
- Delete: `db/watcher_status.go`
- Test: existing tests for those commands + `go build`

**Interfaces:**
- Consumes: `wdb.GetPollerStatus(conn, name) (*wdb.PollerStatus, error)`, `wdb.HasPollerError(conn, name) bool`.
- Produces: all watcher-status reads come from `watcher_poller_status` via the library.

- [ ] **Step 1: Confirm the call sites**

Run: `grep -rn "GetWatcherStatus\|HasWatcherError" cmd/`
Expected list: statusline.go, watching.go, triage.go, status.go, api/resources.go.

- [ ] **Step 2: Replace each call**

In each file, replace `d.GetWatcherStatus(name)` → `wdb.GetPollerStatus(d.Conn(), name)` and `d.HasWatcherError(name)` → `wdb.HasPollerError(d.Conn(), name)`. Map the returned `*wdb.PollerStatus` fields (Name/LastSuccess/LastError/LastErrorMessage) to whatever each caller displays (the field names match handler's old `WatcherStatus`). Add the `wdb` import to each file.

- [ ] **Step 3: Delete the dead db file**

Delete `db/watcher_status.go` (the handler `GetWatcherStatus`/`RecordWatcher*`/`HasWatcherError` on `*db.DB`). NOTE: the writers (`RecordWatcherSuccess/Error`) were only called by handler's in-tree pollers, which Task 9 replaces with the library pollers (which write `watcher_poller_status` themselves). Confirm via `grep -rn "RecordWatcherSuccess\|RecordWatcherError\|GetWatcherStatus\|HasWatcherError" .` that nothing outside the (about-to-be-wrapped) `cmd/watcher` and the just-edited readers references them. The old `watcher_status` TABLE stays (2c drops it); only the Go accessor file is deleted.

- [ ] **Step 4: Build + test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS (compile confirms all readers repointed).

- [ ] **Step 5: Commit**

```bash
git add cmd/statusline.go cmd/watching.go cmd/triage.go cmd/status.go cmd/api/resources.go
git rm db/watcher_status.go
git commit -s -m "feat: read poller status from watcher_poller_status via the library"
```

---

### Task 9: Watcher command wrappers over the library pollers + auth.yaml credentials

**Files:**
- Modify: `cmd/watcher/run.go`, `cmd/watcher/auth.go`, and the other `cmd/watcher/*.go` as needed
- Test: existing `cmd/watcher` tests + `go build`

**Interfaces:**
- Consumes: `github/jira` library pollers, `wdb.ActiveResources`, library `config` (auth.yaml).
- Produces: `handler watcher run <svc>` polls via the library into `watcher_*`; credentials read from `~/.config/watcher/auth.yaml`.

- [ ] **Step 1: Rewrite `run`**

In `cmd/watcher/run.go`, replace the in-tree `watcher.ActiveResources` + `github.Poll`/`jira.Poll` (handler's) with the library equivalents: resolve creds via the library `config` package (`config.Load(config.DefaultPath())` → `cfg.GitHub()`/`cfg.Jira()`), resolve resources via `wdb.ActiveResources(d.Conn(), resourceType)`, and call the library `github.Poll(d.Conn(), token, resources, logger)` / `jira.Poll(d.Conn(), jiraAuth, resources, logger)`. Map handler's service→resourceType (github→"pr", jira→"jira") as today.

- [ ] **Step 2: Rewrite `auth`**

In `cmd/watcher/auth.go`, write credentials via the library `config` package to `auth.yaml` (config.Save) instead of handler's `~/.agent-handler/config.yaml`. Keep the interactive token prompts. Move Jira `custom_fields`/`bot_usernames` into the library config shape.

- [ ] **Step 3: Keep scheduling as-is**

`install`/`stop`/`start`/`uninstall`/`list` continue to use handler's existing launchd/cron (invoking `handler watcher run <name>`). Do NOT change them to the library scheduler. Verify they still compile against any moved code.

- [ ] **Step 4: Build + test + manual smoke**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. (Live polling needs real creds/network — not run in CI; the reviewer checks wiring, not a live poll.)

- [ ] **Step 5: Commit**

```bash
git add cmd/watcher/
git commit -s -m "feat(watcher): thin command wrappers over the library pollers; auth.yaml creds"
```

---

### Task 10: The production data-migration command (`handler setup --migrate-watcher`)

The most carefully specified task. It runs ONCE on the real DB, via the human, per the runbook. Everything here is tested against TEMP databases only.

**Files:**
- Create: `cmd/migrate_watcher.go`
- Modify: `cmd/setup.go` (add the `--migrate-watcher` flag/subcommand)
- Test: `cmd/migrate_watcher_test.go`

**Interfaces:**
- Consumes: all prior tasks; `wdb.*` insert/subscribe functions.
- Produces: a command that copies handler's watcher data into `watcher_*`, sets `watcher_migrated=1`, with backup + verify + refuse-if-watchers-active guards.

- [ ] **Step 1: Write the migration core as a testable function**

Create `cmd/migrate_watcher.go` with `func migrateWatcherData(d *db.DB) (MigrationReport, error)` (pure data copy; NO backup/prompt — those wrap it in the command). It performs, in order, inside a transaction where feasible:
1. copy `events WHERE source IN ('github','jira')` → `watcher_events` (+ their `event_resources` → `watcher_event_resources`).
2. `resource_state` → `watcher_resource_state`.
3. `subscriptions` → `watcher_subscriptions`: subscriber `handler:session:<session_id>`, map `deleted_at`, `unsubscribed_by='user'` → `unsubscribed_by_user=1`, `expires_at = now+5d` for rows whose session is active else leave null/expired.
4. `resource_relationships` → `watcher_resource_relationships`.
5. handler `watcher_status` → `watcher_poller_status` (leave original).
6. `setWatcherMigrated()`.
`MigrationReport` holds before/after counts for each table.

- [ ] **Step 2: Write the failing test (temp DB, seeded)**

Create `cmd/migrate_watcher_test.go`: open a temp `db.DB`, seed via handler's own inserts a couple github/jira events (+event_resources), an agent event, a resource_state row, a couple subscriptions (one user-unsubscribed), a relationship, a watcher_status row. Run `migrateWatcherData`. Assert: `watcher_events` count == the github+jira event count (agent event NOT copied); `watcher_subscriptions` has the subscriptions with correct subscriber strings and the user-unsubscribed one has `unsubscribed_by_user=1`; `watcher_poller_status` copied; `watcherMigrationDone()` true; the report counts match.

- [ ] **Step 3: Run to verify fail, implement, pass**

Run: `go test ./cmd/ -run TestMigrateWatcherData -v` (FAIL → implement → PASS). Verify via a second assertion that re-running `migrateWatcherData` is safe/idempotent OR that the command guards against double-run (the marker check); document which. Prefer: the command refuses if `watcherMigrationDone()` is already true.

- [ ] **Step 4: Wrap with backup + guards in the command**

Add the cobra wiring in `cmd/setup.go` (`--migrate-watcher`). The command:
1. Refuses if `watcherMigrationDone()` already true (print "already migrated").
2. Refuses if any watcher is currently installed AND running (reuse handler's scheduler `IsRunning` check); instruct `handler watcher stop`.
3. Backs up the DB file: copy `<dbpath>` → `<dbpath>.backup-<UTC-timestamp>`; abort on any error; print the backup path.
4. Prints pre-counts.
5. Calls `migrateWatcherData`.
6. Prints the post-counts report and a PASS/FAIL comparison.
7. Migrates credentials `~/.agent-handler/config.yaml` → `~/.config/watcher/auth.yaml` via the library config (only if the auth.yaml lacks them).
8. Prints next steps (start watchers; verify `handler watching`/`status`).

- [ ] **Step 5: Test the command guards**

Add tests: double-run refused; backup file created (point the command at a temp db path and assert the `.backup-*` file exists); counts report populated. Do NOT test the live scheduler stop (mock or skip that guard in the test via a flag).

- [ ] **Step 6: Run tests + build**

Run: `go test ./... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/migrate_watcher.go cmd/migrate_watcher_test.go cmd/setup.go
git commit -s -m "feat(setup): --migrate-watcher command with backup, verify, guards"
```

- [ ] **Step 8: Write the runbook doc**

Create `docs/watcher-migration-runbook.md` capturing the exact human steps: `handler watcher stop`; back up (the command also does this); `go build && make install` (or however handler installs); `handler setup --migrate-watcher`; verify `handler health`/`watching`/`status`/`log --global` and unread counts; `handler watcher start`; and the ROLLBACK procedure (stop watchers; restore `<dbpath>.backup-<ts>`; `git checkout <prev-commit> && rebuild && install`; start watchers). Commit it with this task.

---

### Task 11: Pin the library, remove replace, final verification

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Remove the replace, confirm the pin**

Run:
```bash
go mod edit -dropreplace github.com/mturley/watcher
go get github.com/mturley/watcher@v0.2.0
go mod tidy
```
Expected: `go.mod` has `require github.com/mturley/watcher v0.2.0` and NO replace line.

- [ ] **Step 2: Full verification**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS with the real published v0.2.0. (Requires GOPRIVATE/SSH for the private module, or the repo being public — the human confirms fetch works.)

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -s -m "build: pin watcher v0.2.0, drop local replace"
```

- [ ] **Step 4: STOP — human-gated release**

Do NOT tag or push. The production migration (running `handler setup --migrate-watcher` against the real DB) and any handler release tag are human-run via the runbook. Surface completion to the controller/human.

---

## Self-Review

**Spec coverage (Phase 2b section):**
- Import v0.2.0 via replace + Migrate alongside runMigrations, disjoint → Task 1.
- Subscriber format helpers → Task 2.
- Subscription writes over watcher_subscriptions (all 7 methods incl. composed Restore honoring UnsubscribedByUser) → Task 3.
- Subscription/resource reads (ListSubscriptions, SessionsForResource, FindRelatedSessions, ResourceHistory) → Task 4.
- Lease wiring (register/statusline via reused methods; heartbeat renew; unregister via mapped SoftDelete; cleanup revoke — the latent bug) → Task 5.
- Inbox UNION, both arms, dismissal on both, gated on the data-migration marker (NOT schema presence) → Task 6.
- Cursor max(ts) fix → Task 7.
- watcher_status → watcher_poller_status repoint + delete db/watcher_status.go → Task 8.
- cmd/watcher wrappers + auth.yaml creds; keep launchd/cron → Task 9.
- Migration command with backup/verify/refuse-if-active/rollback runbook → Task 10.
- Pin v0.2.0, drop replace → Task 11.
- Old tables + legacy path RETAINED (2c deletes) → enforced in Global Constraints and Task 6 (legacy path kept behind gate); no task deletes subscriptions/resource_state/resource_relationships/watcher_status tables.

**Placeholder scan:** No TBD/TODO. Two places defer to "inspect the real function name" (Task 7 cursor API, Task 6 caller columns) — these are grounded by the verified line anchors (events.go:190/221/273/323/385, cursors.go:156) and are read-the-signature instructions, not vague placeholders; the surrounding code is fully specified.

**Type consistency:** `handlerSubscriber`/`sessionIDFromSubscriber`/`sessionLeaseTTL` defined in Task 2, used in Tasks 3–5, 10. `wdb` alias used consistently. Library signatures (`Subscribe`, `SubscribeOpts{TTL,Backfill,IfAbsent}`, `UserUnsubscribe`, `Reinstate`, `Renew`, `RevokePrefix`, `AllSubscriptions`/`ActiveSubscriptions(_, _, prefix bool)`, `SubscribersOf`, `EventsForResource`, `GetPollerStatus`/`HasPollerError`, `SchemaVersion`, `Migrate`, `InsertEvent`) match the shipped v0.2 API verified against ~/git/watcher. Handler's `Conn()` accessor confirmed at db/db.go:130. The gate uses a handler-owned `handler_meta` marker (Task 6), deliberately distinct from `wdb.SchemaVersion` — called out as CRITICAL because gating on schema presence would switch reads before data exists.

**One risk flagged for the executor:** the gate marker. Task 1 makes `watcher_*` exist immediately (SchemaVersion≥1) but the DATA isn't there until Task 10 runs. Every read gate MUST use `watcherMigrationDone()` (the `handler_meta` flag set only by the migration command), never `SchemaVersion`. This is stated in Task 6 Step 5 and the Global Constraints. A reviewer should treat gating-on-SchemaVersion as a Critical finding.
