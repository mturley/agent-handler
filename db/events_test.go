package db

import (
	"testing"
	"time"
)

func TestInsertAndQueryEvents(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-event-test")

	now := time.Now().UTC().Format(time.RFC3339)
	e := Event{
		ID:         "event-1",
		TS:         now,
		ExternalTS: strPtr("2026-06-15T10:00:00Z"),
		Source:     "github",
		SessionID:  strPtr("session-event-test"),
		Type:       "pr_opened",
		Title:      "New PR opened",
		Body:       strPtr("PR body text"),
		Author:     strPtr("alice"),
		AuthorType: strPtr("user"),
		Broadcast:  false,
		Tags:       strPtr("tag1,tag2"),
	}

	recipients := []EventRecipient{
		{RecipientType: "session", RecipientValue: "session-event-test"},
	}

	resources := []EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#123", ResourceURL: strPtr("https://github.com/owner/repo/pull/123")},
	}

	if err := d.InsertEvent(e, recipients, resources); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Query by session
	filter := EventFilter{SessionID: strPtr("session-event-test")}
	events, err := d.QueryEvents(filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	got := events[0]
	if got.ID != e.ID {
		t.Errorf("ID: got %q, want %q", got.ID, e.ID)
	}
	if got.TS != e.TS {
		t.Errorf("TS: got %q, want %q", got.TS, e.TS)
	}
	if got.ExternalTS == nil || *got.ExternalTS != *e.ExternalTS {
		t.Errorf("ExternalTS: got %v, want %v", got.ExternalTS, e.ExternalTS)
	}
	if got.Source != e.Source {
		t.Errorf("Source: got %q, want %q", got.Source, e.Source)
	}
	if got.SessionID == nil || *got.SessionID != *e.SessionID {
		t.Errorf("SessionID: got %v, want %v", got.SessionID, e.SessionID)
	}
	if got.Type != e.Type {
		t.Errorf("Type: got %q, want %q", got.Type, e.Type)
	}
	if got.Title != e.Title {
		t.Errorf("Title: got %q, want %q", got.Title, e.Title)
	}
	if got.Body == nil || *got.Body != *e.Body {
		t.Errorf("Body: got %v, want %v", got.Body, e.Body)
	}
	if got.Author == nil || *got.Author != *e.Author {
		t.Errorf("Author: got %v, want %v", got.Author, e.Author)
	}
	if got.AuthorType == nil || *got.AuthorType != *e.AuthorType {
		t.Errorf("AuthorType: got %v, want %v", got.AuthorType, e.AuthorType)
	}
	if got.Broadcast != e.Broadcast {
		t.Errorf("Broadcast: got %v, want %v", got.Broadcast, e.Broadcast)
	}
	if got.Tags == nil || *got.Tags != *e.Tags {
		t.Errorf("Tags: got %v, want %v", got.Tags, e.Tags)
	}
}

func TestUnreadViaResourceSubscription(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-unread-sub")

	// Subscribe to a PR
	now := time.Now().UTC().Format(time.RFC3339)
	sub := Subscription{
		ID:           "sub-unread-1",
		SessionID:    "session-unread-sub",
		ResourceType: "pr",
		ResourceID:   "owner/repo#200",
		CreatedAt:    now,
	}
	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Set cursor
	cursorTS := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := d.AdvanceCursor("session-unread-sub", cursorTS); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Insert event referencing the subscribed PR
	eventTS := time.Now().UTC().Format(time.RFC3339)
	e := Event{
		ID:        "event-unread-1",
		TS:        eventTS,
		Source:    "github",
		Type:      "pr_comment",
		Title:     "New comment on PR",
		Broadcast: false,
	}
	resources := []EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#200"},
	}

	if err := d.InsertEvent(e, nil, resources); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Check unread
	unread, err := d.UnreadForSession("session-unread-sub")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}

	if len(unread) != 1 {
		t.Fatalf("expected 1 unread event, got %d", len(unread))
	}

	if unread[0].ID != e.ID {
		t.Errorf("unread event ID: got %q, want %q", unread[0].ID, e.ID)
	}
}

func TestUnreadViaDirectRecipient(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-unread-direct")

	// Set cursor
	cursorTS := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := d.AdvanceCursor("session-unread-direct", cursorTS); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Insert event addressed to the session
	eventTS := time.Now().UTC().Format(time.RFC3339)
	e := Event{
		ID:        "event-unread-2",
		TS:        eventTS,
		Source:    "system",
		Type:      "notification",
		Title:     "Direct notification",
		Broadcast: false,
	}
	recipients := []EventRecipient{
		{RecipientType: "session", RecipientValue: "session-unread-direct"},
	}

	if err := d.InsertEvent(e, recipients, nil); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Check unread
	unread, err := d.UnreadForSession("session-unread-direct")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}

	if len(unread) != 1 {
		t.Fatalf("expected 1 unread event, got %d", len(unread))
	}

	if unread[0].ID != e.ID {
		t.Errorf("unread event ID: got %q, want %q", unread[0].ID, e.ID)
	}
}

func TestUnreadViaBroadcast(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-unread-broadcast")

	// Set cursor
	cursorTS := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := d.AdvanceCursor("session-unread-broadcast", cursorTS); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Insert broadcast event
	eventTS := time.Now().UTC().Format(time.RFC3339)
	e := Event{
		ID:        "event-unread-3",
		TS:        eventTS,
		Source:    "system",
		Type:      "announcement",
		Title:     "System announcement",
		Broadcast: true,
	}

	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Check unread
	unread, err := d.UnreadForSession("session-unread-broadcast")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}

	if len(unread) != 1 {
		t.Fatalf("expected 1 unread event, got %d", len(unread))
	}

	if unread[0].ID != e.ID {
		t.Errorf("unread event ID: got %q, want %q", unread[0].ID, e.ID)
	}
}

func TestUnreadExcludesOldEvents(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-unread-old")

	// Insert event
	oldEventTS := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	e := Event{
		ID:        "event-old",
		TS:        oldEventTS,
		Source:    "system",
		Type:      "announcement",
		Title:     "Old announcement",
		Broadcast: true,
	}

	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Set cursor AFTER event timestamp
	cursorTS := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := d.AdvanceCursor("session-unread-old", cursorTS); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Check unread - should be 0
	unread, err := d.UnreadForSession("session-unread-old")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}

	if len(unread) != 0 {
		t.Errorf("expected 0 unread events, got %d", len(unread))
	}
}
