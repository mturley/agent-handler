package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/mturley/agent-handler/cronsched"
	"github.com/mturley/agent-handler/db"
)

// cronPromptWidth keeps a job line inside a terminal width without wrapping;
// the statusline is one line per job.
const cronPromptWidth = 60

// formatCronDelay describes how long until `to`, phrased to lead a statusline
// job entry ("In 5 minutes"). A time already past reads "Now" — a one-shot on
// the cusp of firing is not negative, it is imminent.
func formatCronDelay(from, to time.Time) string {
	d := to.Sub(from)
	if d <= 0 {
		return "Now"
	}
	if d < time.Minute {
		return "In under a minute"
	}
	if mins := int(d.Round(time.Minute).Minutes()); mins < 60 {
		return fmt.Sprintf("In %s", plural(mins, "minute"))
	}
	if hours := int(d.Round(time.Hour).Hours()); hours < 24 {
		return fmt.Sprintf("In %s", plural(hours, "hour"))
	}
	return fmt.Sprintf("In %s", plural(int(d.Round(24*time.Hour).Hours()/24), "day"))
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// cronStatusLine is one job as the statusline shows it. The label and the
// prompt are kept apart because they are coloured differently: the label is
// what the eye scans for, the prompt is supporting detail.
type cronStatusLine struct {
	Label  string // "In 5 minutes (one-shot)"
	Prompt string // truncated to a single line
}

// cronStatusSection renders the statusline's cron section as plain text: a
// header counting the jobs, then one entry per job, soonest first. Colour is
// applied by the caller so this stays assertable.
//
// Returns nothing when there are no jobs — the section is omitted entirely
// rather than showing a zero.
func cronStatusSection(crons []db.SessionCron, now time.Time) (string, []cronStatusLine) {
	if len(crons) == 0 {
		return "", nil
	}

	type entry struct {
		next  time.Time
		known bool
		line  cronStatusLine
	}
	entries := make([]entry, 0, len(crons))
	for _, c := range crons {
		recurrence := "one-shot"
		if c.Recurring {
			recurrence = "recurring"
		}
		next, known := cronsched.Next(c.Schedule, now)
		when := "Next fire unknown"
		if known {
			when = formatCronDelay(now, next)
		}
		entries = append(entries, entry{
			next:  next,
			known: known,
			line: cronStatusLine{
				Label:  fmt.Sprintf("%s (%s)", when, recurrence),
				Prompt: truncatePrompt(c.Prompt, cronPromptWidth),
			},
		})
	}

	// Soonest first; jobs with an unreadable schedule have no place in that
	// ordering, so they go last.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].known != entries[j].known {
			return entries[i].known
		}
		return entries[i].next.Before(entries[j].next)
	})

	lines := make([]cronStatusLine, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.line)
	}
	return plural(len(crons), "active cron job"), lines
}

// renderCronsSection prints the session's active Claude Code cron jobs at the
// bottom of the statusline. Failures are silent: a statusline must never break
// the prompt over a secondary section.
func renderCronsSection(d *db.DB, session *db.Session) {
	if d == nil || session == nil {
		return
	}
	crons, err := d.ListSessionCrons(session.SessionID)
	if err != nil || len(crons) == 0 {
		return
	}
	header, jobs := cronStatusSection(crons, time.Now())
	fmt.Printf("%s%s%s\n", colorBoldCyan, header, colorReset)
	for _, j := range jobs {
		fmt.Printf("%s  ↳ %s%s%s%s%s: %s%s\n",
			colorDim, colorReset,
			colorCyan, j.Label, colorReset,
			colorDim, j.Prompt, colorReset)
	}
}
