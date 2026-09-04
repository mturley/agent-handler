package cmd

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

func TestCronStatusLinesEmptyWithNoJobs(t *testing.T) {
	header, jobs := cronStatusSection(nil, time.Now())
	if header != "" || jobs != nil {
		t.Errorf("expected nothing with no jobs, got %q %v", header, jobs)
	}
}

func TestCronStatusLinesHeaderCountsJobs(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	header, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "a", Schedule: "5 13 * * *", Prompt: "first"},
		{JobID: "b", Schedule: "32 13 * * *", Prompt: "second", Recurring: true},
	}, now)

	if len(jobs) != 2 {
		t.Fatalf("expected one line per job, got %d: %v", len(jobs), jobs)
	}
	if header != "2 active cron jobs" {
		t.Errorf("header = %q", header)
	}
}

// A single job reads "1 active cron job", not "1 active cron jobs".
func TestCronStatusLinesSingularHeader(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	header, _ := cronStatusSection([]db.SessionCron{
		{JobID: "a", Schedule: "5 13 * * *", Prompt: "only"},
	}, now)
	if header != "1 active cron job" {
		t.Errorf("header = %q, want singular", header)
	}
}

func TestCronStatusLineFormat(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	_, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "a", Schedule: "5 13 * * *", Prompt: "do the thing"},
		{JobID: "b", Schedule: "32 13 * * *", Prompt: "poll for updates", Recurring: true},
	}, now)

	// The label is rendered in its own colour, so it is kept separate from the
	// prompt rather than being one pre-joined string.
	if jobs[0].Label != "In 5 minutes (one-shot)" || jobs[0].Prompt != "do the thing" {
		t.Errorf("one-shot line = %+v", jobs[0])
	}
	if jobs[1].Label != "In 32 minutes (recurring)" || jobs[1].Prompt != "poll for updates" {
		t.Errorf("recurring line = %+v", jobs[1])
	}
}

// Jobs are listed soonest-first, regardless of the order the DB returns them —
// the next one to fire is the one the user cares about.
func TestCronStatusLinesSortedBySoonest(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	_, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "later", Schedule: "50 13 * * *", Prompt: "later"},
		{JobID: "sooner", Schedule: "10 13 * * *", Prompt: "sooner"},
	}, now)
	if jobs[0].Prompt != "sooner" {
		t.Errorf("expected the soonest job first, got %v", jobs)
	}
}

// A job whose schedule we cannot parse still has to be listed — it exists and
// the user should see it — but with no invented fire time. Those sort last.
func TestCronStatusLinesUnparseableSchedule(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	_, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "bad", Schedule: "nonsense", Prompt: "mystery"},
		{JobID: "ok", Schedule: "10 13 * * *", Prompt: "known"},
	}, now)
	if jobs[0].Prompt != "known" {
		t.Errorf("expected the schedulable job first, got %v", jobs)
	}
	if jobs[1].Label != "Next fire unknown (one-shot)" || jobs[1].Prompt != "mystery" {
		t.Errorf("unparseable line = %+v", jobs[1])
	}
}

func TestCronDelayPhrasing(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(20 * time.Second), "In under a minute"},
		{now.Add(time.Minute), "In 1 minute"},
		{now.Add(5 * time.Minute), "In 5 minutes"},
		{now.Add(time.Hour), "In 1 hour"},
		{now.Add(3 * time.Hour), "In 3 hours"},
		{now.Add(48 * time.Hour), "In 2 days"},
		// A fire time already in the past is about to be swept up, not negative.
		{now.Add(-time.Minute), "Now"},
	}
	for _, c := range cases {
		if got := formatCronDelay(now, c.at); got != c.want {
			t.Errorf("delay to %v = %q, want %q", c.at, got, c.want)
		}
	}
}

// The statusline is one line per job; a long prompt must not wrap it.
func TestCronStatusLineTruncatesLongPrompt(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	long := strings.Repeat("x", 200)
	_, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "a", Schedule: "5 13 * * *", Prompt: long},
	}, now)
	if len(jobs[0].Prompt) > 70 {
		t.Errorf("prompt is %d chars, expected truncation: %q", len(jobs[0].Prompt), jobs[0].Prompt)
	}
	if !strings.HasSuffix(jobs[0].Prompt, "…") {
		t.Errorf("expected an ellipsis on the truncated prompt, got %q", jobs[0].Prompt)
	}
}

// Newlines in a prompt would break the statusline's line accounting.
func TestCronStatusLineFlattensNewlines(t *testing.T) {
	now := time.Date(2026, 9, 4, 13, 0, 0, 0, time.Local)
	_, jobs := cronStatusSection([]db.SessionCron{
		{JobID: "a", Schedule: "5 13 * * *", Prompt: "first\nsecond"},
	}, now)
	if strings.Contains(jobs[0].Prompt, "\n") {
		t.Errorf("expected a single line, got %q", jobs[0].Prompt)
	}
}

// --- rendering ---------------------------------------------------------------

// The label carries the information worth scanning for — when the job fires and
// whether it repeats — so it is rendered undimmed, with only the arrow and the
// prompt behind it dimmed.
func TestRenderCronsSectionLabelIsNotDimmed(t *testing.T) {
	d := cronTestDB(t)
	ts := time.Now().UTC().Format(time.RFC3339)
	next := time.Now().Add(5 * time.Minute)
	if err := d.UpsertSessionCron("r-1", db.SessionCron{
		JobID: "a", Schedule: next.Format("4 15") + " * * *", Prompt: "do the thing",
	}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out := captureStdout(t, func() {
		renderCronsSection(d, &db.Session{SessionID: "r-1"})
	})

	// Matched loosely: the exact minute count depends on where in the current
	// minute the test runs.
	loc := regexp.MustCompile(`In \d+ minutes \(one-shot\)`).FindStringIndex(out)
	if loc == nil {
		t.Fatalf("expected a one-shot label in output, got %q", out)
	}
	idx := loc[0]
	// Whatever escape sequence immediately precedes the label must not be the
	// dim code — that is the whole point of the split.
	prefix := out[:idx]
	lastEsc := strings.LastIndex(prefix, "\033[")
	if lastEsc < 0 {
		t.Fatalf("expected the label to be colourised, got %q", out)
	}
	if strings.HasPrefix(prefix[lastEsc:], colorDim) {
		t.Errorf("label is dimmed; escape before it was %q in %q", prefix[lastEsc:], out)
	}

	// The prompt after it is still dim.
	if !strings.Contains(out, colorDim+": do the thing") {
		t.Errorf("expected the prompt to stay dimmed, got %q", out)
	}
}
