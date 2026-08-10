package db

import (
	"testing"

	wdb "github.com/mturley/watcher/db"
)

func TestSubscribeAndActive(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}

	subs, err := wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)
	if err != nil {
		t.Fatalf("ActiveSubscriptions failed: %v", err)
	}
	if len(subs) != 1 || subs[0].Resource.ID != "o/r#1" {
		t.Fatalf("got %+v", subs)
	}
}

func TestUnsubscribeIsUserProtected(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}
	if err := d.Unsubscribe("s1", "pr", "o/r#1"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// user-unsubscribe must NOT be revived by SubscribeIfNew
	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("second SubscribeIfNew failed: %v", err)
	}

	subs, err := wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)
	if err != nil {
		t.Fatalf("ActiveSubscriptions failed: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("user-unsubscribed resource must stay inactive, got %+v", subs)
	}
}

func TestSubscribeForceRevivesUserUnsubscribe(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}
	if err := d.Unsubscribe("s1", "pr", "o/r#1"); err != nil { // user tombstone
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// An explicit Subscribe (e.g. `handler subscribe` / /watch) must override
	// the prior user unwatch.
	if err := d.Subscribe(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	subs, err := wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)
	if err != nil {
		t.Fatalf("ActiveSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected explicit Subscribe to force-revive the user-unsubscribed resource, got %+v", subs)
	}
}

func TestRestoreSkipsUserUnsubscribed(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "keep"}); err != nil {
		t.Fatalf("SubscribeIfNew keep failed: %v", err)
	}
	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "user-gone"}); err != nil {
		t.Fatalf("SubscribeIfNew user-gone failed: %v", err)
	}
	if err := d.Unsubscribe("s1", "pr", "user-gone"); err != nil { // user tombstone
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	if _, err := d.SoftDeleteSubscriptionsForSession("s1"); err != nil { // session end (non-user revoke of the rest)
		t.Fatalf("SoftDeleteSubscriptionsForSession failed: %v", err)
	}
	n, err := d.RestoreSubscriptionsForSession("s1") // session return
	if err != nil {
		t.Fatalf("RestoreSubscriptionsForSession failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 restored subscription, got %d", n)
	}

	subs, err := wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)
	if err != nil {
		t.Fatalf("ActiveSubscriptions failed: %v", err)
	}
	// "keep" comes back; "user-gone" stays gone
	if len(subs) != 1 || subs[0].Resource.ID != "keep" {
		t.Fatalf("restore should revive only non-user subs, got %+v (n=%d)", subs, n)
	}
}

func TestSoftDeleteForSessionReturnsCount(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}
	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#2"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}

	n, err := d.SoftDeleteSubscriptionsForSession("s1")
	if err != nil {
		t.Fatalf("SoftDeleteSubscriptionsForSession failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 soft-deleted, got %d", n)
	}

	subs, err := wdb.ActiveSubscriptions(d.Conn(), handlerSubscriber("s1"), false)
	if err != nil {
		t.Fatalf("ActiveSubscriptions failed: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 active after soft-delete, got %+v", subs)
	}
}
