package terminal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeDetect returns the queued fake results, incrementing a counter, for
// controlling detectFn in tests.
func fakeDetect(results ...[3]string) (func() (string, string, string), *int) {
	calls := 0
	fn := func() (string, string, string) {
		idx := calls
		if idx >= len(results) {
			idx = len(results) - 1
		}
		calls++
		r := results[idx]
		return r[0], r[1], r[2]
	}
	return fn, &calls
}

func TestDetectCachedHit(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", "ws:1"})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	fixedNow := time.Now()
	now = func() time.Time { return fixedNow }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	t1, t2, t3 := DetectCached(dir, "sess-a")
	t4, t5, t6 := DetectCached(dir, "sess-a")

	if *calls != 1 {
		t.Fatalf("expected 1 call, got %d", *calls)
	}
	if t1 != "cmux" || t2 != "surface:1" || t3 != "ws:1" {
		t.Fatalf("unexpected first result: %q %q %q", t1, t2, t3)
	}
	if t4 != t1 || t5 != t2 || t6 != t3 {
		t.Fatalf("second call didn't match first: %q %q %q", t4, t5, t6)
	}
}

func TestDetectCachedMissesOnEnvChange(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", "ws:1"}, [3]string{"cmux", "surface:2", "ws:2"})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	fixedNow := time.Now()
	now = func() time.Time { return fixedNow }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	t.Setenv("CMUX_SURFACE_ID", "surface:1")
	DetectCached(dir, "sess-a")

	t.Setenv("CMUX_SURFACE_ID", "surface:2")
	tt, tid, wid := DetectCached(dir, "sess-a")

	if *calls != 2 {
		t.Fatalf("expected 2 calls, got %d", *calls)
	}
	if tt != "cmux" || tid != "surface:2" || wid != "ws:2" {
		t.Fatalf("expected second fake result, got %q %q %q", tt, tid, wid)
	}
}

func TestDetectCachedExpires(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", "ws:1"})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	base := time.Now()
	cur := base
	now = func() time.Time { return cur }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	DetectCached(dir, "sess-a")

	cur = base.Add(4*time.Minute + 59*time.Second)
	DetectCached(dir, "sess-a")
	if *calls != 1 {
		t.Fatalf("expected still 1 call within TTL, got %d", *calls)
	}

	cur = base.Add(5*time.Minute + 1*time.Second)
	DetectCached(dir, "sess-a")
	if *calls != 2 {
		t.Fatalf("expected 2 calls after TTL expiry, got %d", *calls)
	}
}

func TestDetectCachedDoesNotCacheEmpty(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", ""})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	fixedNow := time.Now()
	now = func() time.Time { return fixedNow }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	DetectCached(dir, "sess-a")
	DetectCached(dir, "sess-a")
	if *calls != 2 {
		t.Fatalf("expected 2 calls (no caching of empty workspace), got %d", *calls)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Errorf("expected no cache files written, got %v", ents)
	}

	fn2, calls2 := fakeDetect([3]string{"", "", ""})
	detectFn = fn2
	dir2 := t.TempDir()
	DetectCached(dir2, "sess-b")
	DetectCached(dir2, "sess-b")
	if *calls2 != 2 {
		t.Fatalf("expected 2 calls (no caching of fully empty result), got %d", *calls2)
	}
	ents2, _ := os.ReadDir(dir2)
	if len(ents2) != 0 {
		t.Errorf("expected no cache files written, got %v", ents2)
	}
}

func TestDetectCachedPerSession(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", "ws:1"})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	fixedNow := time.Now()
	now = func() time.Time { return fixedNow }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	DetectCached(dir, "a")
	DetectCached(dir, "b")
	if *calls != 2 {
		t.Fatalf("expected 2 calls for distinct sessions, got %d", *calls)
	}
}

func TestDetectCachedEmptySessionUncached(t *testing.T) {
	fn, calls := fakeDetect([3]string{"cmux", "surface:1", "ws:1"})
	oldFn, oldNow := detectFn, now
	detectFn = fn
	fixedNow := time.Now()
	now = func() time.Time { return fixedNow }
	defer func() { detectFn = oldFn; now = oldNow }()

	dir := t.TempDir()
	DetectCached(dir, "")
	DetectCached(dir, "")
	if *calls != 2 {
		t.Fatalf("expected 2 calls for empty session id, got %d", *calls)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 0 {
		t.Errorf("expected no cache files for empty session id, got %v", ents)
	}
	_ = filepath.Join(dir, "x")
}
