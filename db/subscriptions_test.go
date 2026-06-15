package db

import (
	"testing"
	"time"
)

func TestSubscribeAndList(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-sub-test")

	now := time.Now().UTC().Format(time.RFC3339)
	sub := Subscription{
		ID:           "sub-1",
		SessionID:    "session-sub-test",
		ResourceType: "pr",
		ResourceID:   "owner/repo#123",
		ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		CreatedAt:    now,
	}

	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	subs, err := d.ListSubscriptions("session-sub-test", false)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}

	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}

	s := subs[0]
	if s.ID != sub.ID {
		t.Errorf("ID: got %q, want %q", s.ID, sub.ID)
	}
	if s.SessionID != sub.SessionID {
		t.Errorf("SessionID: got %q, want %q", s.SessionID, sub.SessionID)
	}
	if s.ResourceType != sub.ResourceType {
		t.Errorf("ResourceType: got %q, want %q", s.ResourceType, sub.ResourceType)
	}
	if s.ResourceID != sub.ResourceID {
		t.Errorf("ResourceID: got %q, want %q", s.ResourceID, sub.ResourceID)
	}
	if s.ResourceURL == nil || *s.ResourceURL != *sub.ResourceURL {
		t.Errorf("ResourceURL: got %v, want %v", s.ResourceURL, sub.ResourceURL)
	}
	if s.DeletedAt != nil {
		t.Errorf("DeletedAt: got %v, want nil", s.DeletedAt)
	}
}

func TestUnsubscribeSoftDeletes(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-unsub-test")

	now := time.Now().UTC().Format(time.RFC3339)
	sub := Subscription{
		ID:           "sub-2",
		SessionID:    "session-unsub-test",
		ResourceType: "pr",
		ResourceID:   "owner/repo#456",
		CreatedAt:    now,
	}

	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Unsubscribe
	if err := d.Unsubscribe("session-unsub-test", "pr", "owner/repo#456"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Should have 0 active subscriptions
	activeSubs, err := d.ListSubscriptions("session-unsub-test", false)
	if err != nil {
		t.Fatalf("ListSubscriptions(false) failed: %v", err)
	}
	if len(activeSubs) != 0 {
		t.Errorf("expected 0 active subscriptions, got %d", len(activeSubs))
	}

	// Should have 1 total (including deleted)
	allSubs, err := d.ListSubscriptions("session-unsub-test", true)
	if err != nil {
		t.Fatalf("ListSubscriptions(true) failed: %v", err)
	}
	if len(allSubs) != 1 {
		t.Errorf("expected 1 total subscription, got %d", len(allSubs))
	}
	if allSubs[0].DeletedAt == nil {
		t.Error("expected DeletedAt to be set, got nil")
	}
}

func TestReinstateSubscription(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-reinstate-test")

	now := time.Now().UTC().Format(time.RFC3339)
	sub := Subscription{
		ID:           "sub-3",
		SessionID:    "session-reinstate-test",
		ResourceType: "pr",
		ResourceID:   "owner/repo#789",
		CreatedAt:    now,
	}

	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Unsubscribe
	if err := d.Unsubscribe("session-reinstate-test", "pr", "owner/repo#789"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Reinstate
	if err := d.Reinstate("session-reinstate-test", "pr", "owner/repo#789"); err != nil {
		t.Fatalf("Reinstate failed: %v", err)
	}

	// Should have 1 active subscription
	subs, err := d.ListSubscriptions("session-reinstate-test", false)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 active subscription after reinstate, got %d", len(subs))
	}
	if subs[0].DeletedAt != nil {
		t.Errorf("expected DeletedAt to be nil after reinstate, got %v", subs[0].DeletedAt)
	}
}

func TestSubscribeDeduplicate(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-dedup-test")

	now := time.Now().UTC().Format(time.RFC3339)
	sub1 := Subscription{
		ID:           "sub-4",
		SessionID:    "session-dedup-test",
		ResourceType: "pr",
		ResourceID:   "owner/repo#100",
		CreatedAt:    now,
	}

	if err := d.Subscribe(sub1); err != nil {
		t.Fatalf("first Subscribe failed: %v", err)
	}

	// Subscribe again with different ID (should deduplicate)
	sub2 := Subscription{
		ID:           "sub-5",
		SessionID:    "session-dedup-test",
		ResourceType: "pr",
		ResourceID:   "owner/repo#100",
		CreatedAt:    now,
	}

	if err := d.Subscribe(sub2); err != nil {
		t.Fatalf("second Subscribe failed: %v", err)
	}

	// Should only have 1 subscription
	subs, err := d.ListSubscriptions("session-dedup-test", false)
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 subscription after deduplicate, got %d", len(subs))
	}
	// Should be the first one
	if subs[0].ID != sub1.ID {
		t.Errorf("expected first subscription ID %q, got %q", sub1.ID, subs[0].ID)
	}
}
