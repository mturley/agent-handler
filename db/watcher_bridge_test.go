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
