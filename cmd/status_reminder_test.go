package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
)

func settings(enabled bool, turns, hours int) config.StatusReminderSettings {
	return config.StatusReminderSettings{Enabled: enabled, Turns: turns, Hours: hours}
}

func TestDecideStatusReminderBelowBothThresholds(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 9, now.Add(-2*time.Hour), now)
	if d.Fire {
		t.Errorf("expected no reminder at 9 turns / 2 hours, got %+v", d)
	}
}

func TestDecideStatusReminderFiresOnTurns(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 10, now.Add(-1*time.Minute), now)
	if !d.Fire {
		t.Fatal("expected reminder when turns threshold is reached")
	}
	if d.Unit != "turns" {
		t.Errorf("expected unit 'turns', got %q", d.Unit)
	}
	if d.Count != 10 {
		t.Errorf("expected count 10, got %d", d.Count)
	}
}

func TestDecideStatusReminderFiresOnHours(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 2, now.Add(-4*time.Hour), now)
	if !d.Fire {
		t.Fatal("expected reminder when hours threshold is exceeded")
	}
	if d.Unit != "hours" {
		t.Errorf("expected unit 'hours', got %q", d.Unit)
	}
	if d.Count != 4 {
		t.Errorf("expected elapsed hours 4, got %d", d.Count)
	}
}

func TestDecideStatusReminderTurnsWinWhenBothTrip(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 12, now.Add(-5*time.Hour), now)
	if !d.Fire || d.Unit != "turns" {
		t.Errorf("expected turns to take precedence when both trip, got %+v", d)
	}
	if d.Count != 12 {
		t.Errorf("expected count 12, got %d", d.Count)
	}
}

func TestDecideStatusReminderDisabled(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(false, 10, 3), 99, now.Add(-99*time.Hour), now)
	if d.Fire {
		t.Error("expected no reminder when disabled")
	}
}

func TestDecideStatusReminderZeroTurnsDisablesTurnCheckOnly(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 0, 3), 500, now.Add(-1*time.Minute), now)
	if d.Fire {
		t.Errorf("expected turns:0 to disable the turn check, got %+v", d)
	}
	d = decideStatusReminder(settings(true, 0, 3), 500, now.Add(-4*time.Hour), now)
	if !d.Fire || d.Unit != "hours" {
		t.Errorf("expected the hours check to still fire with turns:0, got %+v", d)
	}
}

func TestDecideStatusReminderZeroHoursDisablesHourCheckOnly(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 0), 1, now.Add(-500*time.Hour), now)
	if d.Fire {
		t.Errorf("expected hours:0 to disable the hour check, got %+v", d)
	}
}

func TestDecideStatusReminderIgnoresUnknownBaseline(t *testing.T) {
	// A zero baseline means we have no idea when the last status was; the
	// elapsed-time check must not fire on a bogus multi-millennium duration.
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 1, time.Time{}, now)
	if d.Fire {
		t.Errorf("expected no reminder with an unknown baseline, got %+v", d)
	}
}

func TestDecideStatusReminderIgnoresFutureBaseline(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	d := decideStatusReminder(settings(true, 10, 3), 1, now.Add(time.Hour), now)
	if d.Fire {
		t.Errorf("expected no reminder when the baseline is in the future, got %+v", d)
	}
}

func TestStatusReminderMessageTurns(t *testing.T) {
	msg := statusReminderMessage(statusReminderDecision{Fire: true, Unit: "turns", Count: 12})
	if !strings.Contains(msg, "12 turns since your last emitted status") {
		t.Errorf("message missing turn count: %q", msg)
	}
	if !strings.Contains(msg, "handler emit --type status") {
		t.Errorf("message missing the emit command: %q", msg)
	}
	if !strings.Contains(msg, "Be concise.") {
		t.Errorf("message missing the concision instruction: %q", msg)
	}
}

func TestStatusReminderMessageHours(t *testing.T) {
	msg := statusReminderMessage(statusReminderDecision{Fire: true, Unit: "hours", Count: 5})
	if !strings.Contains(msg, "5 hours since your last emitted status") {
		t.Errorf("message missing hour count: %q", msg)
	}
}

func TestStatusReminderMessageSingular(t *testing.T) {
	msg := statusReminderMessage(statusReminderDecision{Fire: true, Unit: "hours", Count: 1})
	if !strings.Contains(msg, "1 hour since") {
		t.Errorf("expected singular 'hour', got %q", msg)
	}
	msg = statusReminderMessage(statusReminderDecision{Fire: true, Unit: "turns", Count: 1})
	if !strings.Contains(msg, "1 turn since") {
		t.Errorf("expected singular 'turn', got %q", msg)
	}
}

// --- wiring: checkStatusReminder + the UserPromptSubmit hook ---

func reminderTestDB(t *testing.T) *db.DB {
	t.Helper()
	t.Setenv("HANDLER_HOME", t.TempDir())
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seedSession(t *testing.T, d *db.DB, id, role, registeredAt string) *db.Session {
	t.Helper()
	if err := d.UpsertSession(db.Session{
		SessionID:    id,
		Harness:      "claude-code",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		Role:         role,
		LastActive:   registeredAt,
		RegisteredAt: registeredAt,
		JSONLPath:    "/tmp/s.jsonl",
	}); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}
	s, err := d.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	return s
}

func TestCheckStatusReminderFiresAtThreshold(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Format(time.RFC3339))

	for i := 0; i < 9; i++ {
		if msg := checkStatusReminder(d, s, "do a thing", now); msg != "" {
			t.Fatalf("unexpected reminder on turn %d: %q", i+1, msg)
		}
	}
	msg := checkStatusReminder(d, s, "do a thing", now)
	if !strings.Contains(msg, "10 turns since your last emitted status") {
		t.Fatalf("expected a reminder on the 10th turn, got %q", msg)
	}
}

func TestCheckStatusReminderResetsClockAfterFiring(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Add(-9*time.Hour).Format(time.RFC3339))

	if msg := checkStatusReminder(d, s, "hi", now); msg == "" {
		t.Fatal("expected the hours threshold to fire")
	}
	// Immediately after firing it must go quiet rather than nag every turn.
	if msg := checkStatusReminder(d, s, "hi", now); msg != "" {
		t.Errorf("expected silence right after a reminder, got %q", msg)
	}
	prompts, baseline, err := d.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 1 {
		t.Errorf("expected the counter to restart (1 turn since the reminder), got %d", prompts)
	}
	if baseline != now.Format(time.RFC3339) {
		t.Errorf("expected the baseline to move to now, got %q", baseline)
	}
}

func TestCheckStatusReminderSkipsAutoInboxPrompts(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Format(time.RFC3339))

	for i := 0; i < 20; i++ {
		if msg := checkStatusReminder(d, s, "/inbox --auto", now); msg != "" {
			t.Fatalf("automated inbox polls must not count as turns, got %q", msg)
		}
	}
	prompts, _, err := d.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 0 {
		t.Errorf("expected auto-inbox prompts not to bump the counter, got %d", prompts)
	}
}

func TestCheckStatusReminderExemptsHandlerRole(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "handler", now.Add(-99*time.Hour).Format(time.RFC3339))

	for i := 0; i < 20; i++ {
		if msg := checkStatusReminder(d, s, "poll", now); msg != "" {
			t.Fatalf("the handler command-center session must be exempt, got %q", msg)
		}
	}
}

func TestCheckStatusReminderRespectsDisabledConfig(t *testing.T) {
	d := reminderTestDB(t)
	enabled := false
	if err := config.Write(config.DefaultPath(), &config.Config{
		Reminders: &config.RemindersConfig{
			Status: &config.StatusReminderConfig{Enabled: &enabled},
		},
	}); err != nil {
		t.Fatalf("config.Write failed: %v", err)
	}

	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Add(-99*time.Hour).Format(time.RFC3339))
	for i := 0; i < 20; i++ {
		if msg := checkStatusReminder(d, s, "work", now); msg != "" {
			t.Fatalf("expected no reminder when disabled in config, got %q", msg)
		}
	}
}

func TestEmitStatusResetsReminderBaseline(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Add(-9*time.Hour).Format(time.RFC3339))

	for i := 0; i < 5; i++ {
		checkStatusReminder(d, s, "work", now)
	}

	if err := runEmitForTest(t, "s1", "status", "did the thing"); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	prompts, baseline, err := d.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 0 {
		t.Errorf("emitting a status must zero the turn counter, got %d", prompts)
	}
	if baseline == now.Add(-9*time.Hour).Format(time.RFC3339) {
		t.Error("emitting a status must move the time baseline forward")
	}
}

func TestEmitNonStatusLeavesReminderBaseline(t *testing.T) {
	d := reminderTestDB(t)
	now := time.Now().UTC()
	s := seedSession(t, d, "s1", "", now.Format(time.RFC3339))

	for i := 0; i < 5; i++ {
		checkStatusReminder(d, s, "work", now)
	}
	if err := runEmitForTest(t, "s1", "blocked", "waiting on review"); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	prompts, _, err := d.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 5 {
		t.Errorf("only status events reset the counter; got %d", prompts)
	}
}

// runEmitForTest invokes the emit command the way the CLI would.
func runEmitForTest(t *testing.T, sessionID, evtType, title string) error {
	t.Helper()
	emitMessage, emitDetail, emitBody, emitTags = "", "", "", ""
	emitTo, emitBroadcast = nil, false
	emitSource = "agent"
	emitType, emitTitle = evtType, title
	if err := emitCmd.Flags().Set("session-id", sessionID); err != nil {
		t.Fatalf("failed to set session-id flag: %v", err)
	}
	t.Cleanup(func() {
		emitType, emitTitle = "", ""
		emitCmd.Flags().Set("session-id", "")
	})
	return runEmit(emitCmd, nil)
}
