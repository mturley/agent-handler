// Package cronsched parses Claude Code cron expressions and answers when they
// next fire.
//
// It exists so the CLI statusline and the web API agree on the interpretation
// of a schedule string — both read the same session_crons rows.
package cronsched

import (
	"time"

	"github.com/robfig/cron/v3"
)

// parser matches Claude Code's format: standard 5-field cron, no seconds field
// and no descriptors.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Next returns the next time expr fires strictly after from, in from's
// location. It reports false for an expression it cannot parse rather than
// erroring — a job with a bad schedule should still be listed, just without a
// predicted fire time.
func Next(expr string, from time.Time) (time.Time, bool) {
	if expr == "" {
		return time.Time{}, false
	}
	sched, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, false
	}
	next := sched.Next(from)
	if next.IsZero() {
		return time.Time{}, false
	}
	return next.In(from.Location()), true
}
