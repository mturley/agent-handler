package cmd

import (
	"testing"

	"github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

func TestTruncateTitle(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 28, "short"},
		{"  spaced  ", 28, "spaced"},
		{"exactly ten", 11, "exactly ten"},
		{"this is a fairly long thread title", 14, "this is a fair…"},
		{"trailing space cut  here padded out longer", 15, "trailing space…"},
	}
	for _, c := range cases {
		if got := truncateTitle(c.in, c.n); got != c.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestTruncateTitleCountsRunesNotBytes(t *testing.T) {
	// 5 multibyte runes; truncating to 3 must keep 3 runes + ellipsis, not
	// slice mid-byte.
	got := truncateTitle("héllo wörld", 3)
	want := "hél…"
	if got != want {
		t.Errorf("truncateTitle multibyte = %q, want %q", got, want)
	}
}

func TestSlackDisplayTitle_CustomNameThenTitleThenID(t *testing.T) {
	d := nsTestDB(t)
	conn := d.Conn()
	const id = "C1:1.2"

	// Neither meta nor state -> raw id.
	if got := slackDisplayTitle(conn, id); got != id {
		t.Errorf("no meta/state: got %q, want id %q", got, id)
	}

	// Cached first-message title (state) but no custom name -> title.
	if err := wdb.UpsertResourceState(conn, "slack", id, `{"title":"First message here"}`, "2030-01-01T00:00:00Z", "2030-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got := slackDisplayTitle(conn, id); got != "First message here" {
		t.Errorf("state title: got %q, want cached title", got)
	}

	// Custom name wins over cached title.
	if err := wdb.SetResourceMetaAt(conn, watcher.Resource{Type: "slack", ID: id}, "My Thread", "", "2030-02-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got := slackDisplayTitle(conn, id); got != "My Thread" {
		t.Errorf("custom name: got %q, want custom name", got)
	}
}
