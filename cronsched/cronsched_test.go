package cronsched

import (
	"testing"
	"time"
)

func TestNextParsesFiveFieldExpression(t *testing.T) {
	from := time.Date(2026, 9, 4, 13, 20, 0, 0, time.Local)
	next, ok := Next("32 13 * * *", from)
	if !ok {
		t.Fatal("expected a five-field expression to parse")
	}
	want := time.Date(2026, 9, 4, 13, 32, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}
}

// Claude Code schedules in local time, so the result must stay in from's
// location — a UTC answer would render as the wrong clock time.
func TestNextKeepsSourceLocation(t *testing.T) {
	from := time.Date(2026, 9, 4, 13, 20, 0, 0, time.Local)
	next, _ := Next("* * * * *", from)
	if next.Location() != from.Location() {
		t.Errorf("location = %v, want %v", next.Location(), from.Location())
	}
}

// Claude's format has no seconds field and no @descriptors; anything else is
// not something we can predict a fire time for.
func TestNextRejectsUnparseableExpressions(t *testing.T) {
	for _, expr := range []string{"", "not a cron", "@hourly", "0 0 * * * *"} {
		if _, ok := Next(expr, time.Now()); ok {
			t.Errorf("expected %q to be rejected", expr)
		}
	}
}
