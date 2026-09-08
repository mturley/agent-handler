package cmd

import "testing"

func TestUIOpenURLMarksHomeSession(t *testing.T) {
	got := uiOpenURL("http://localhost:8420", "abc-123")
	want := "http://localhost:8420/sessions/abc-123?home=abc-123"
	if got != want {
		t.Errorf("uiOpenURL() = %q, want %q", got, want)
	}
}

func TestUIOpenURLEscapesSessionID(t *testing.T) {
	// Session IDs are UUIDs in practice, but the path and query encodings
	// differ on spaces (%20 vs +) and the query value must round-trip
	// through URLSearchParams.get() on the client.
	got := uiOpenURL("http://localhost:8420", "a b/c")
	want := "http://localhost:8420/sessions/a%20b%2Fc?home=a+b%2Fc"
	if got != want {
		t.Errorf("uiOpenURL() = %q, want %q", got, want)
	}
}

// The main page is opened FOR nothing, so it must never carry the home
// marker — a tab homed to the listing would offer to send the user back to
// the page they are already on. This asymmetry is deliberate; a later
// "append it everywhere" refactor would break it.
func TestUIOpenURLLeavesMainPageUnmarked(t *testing.T) {
	got := uiOpenURL("http://localhost:8420", "")
	want := "http://localhost:8420/"
	if got != want {
		t.Errorf("uiOpenURL() = %q, want %q", got, want)
	}
}
