package config

import "testing"

func TestStatusReminderDefaults(t *testing.T) {
	// No reminders block at all: enabled with the documented defaults.
	cfg := &Config{}
	got := cfg.StatusReminder()
	if !got.Enabled {
		t.Error("expected status reminder enabled by default")
	}
	if got.Turns != 10 {
		t.Errorf("expected default turns 10, got %d", got.Turns)
	}
	if got.Hours != 3 {
		t.Errorf("expected default hours 3, got %d", got.Hours)
	}
}

func TestStatusReminderPartialOverride(t *testing.T) {
	// Setting only turns leaves hours at its default.
	turns := 25
	cfg := &Config{
		Reminders: &RemindersConfig{
			Status: &StatusReminderConfig{Turns: &turns},
		},
	}
	got := cfg.StatusReminder()
	if !got.Enabled {
		t.Error("expected enabled when only turns is set")
	}
	if got.Turns != 25 {
		t.Errorf("expected turns 25, got %d", got.Turns)
	}
	if got.Hours != 3 {
		t.Errorf("expected hours to stay at default 3, got %d", got.Hours)
	}
}

func TestStatusReminderDisabled(t *testing.T) {
	enabled := false
	cfg := &Config{
		Reminders: &RemindersConfig{
			Status: &StatusReminderConfig{Enabled: &enabled},
		},
	}
	if cfg.StatusReminder().Enabled {
		t.Error("expected enabled:false to disable the status reminder")
	}
}

func TestStatusReminderZeroThresholdsDisableIndividualChecks(t *testing.T) {
	zero := 0
	cfg := &Config{
		Reminders: &RemindersConfig{
			Status: &StatusReminderConfig{Turns: &zero},
		},
	}
	got := cfg.StatusReminder()
	if !got.Enabled {
		t.Error("turns:0 disables the turn check, not the whole reminder")
	}
	if got.Turns != 0 {
		t.Errorf("expected turns 0 to be preserved, got %d", got.Turns)
	}
	if got.Hours != 3 {
		t.Errorf("expected hours 3, got %d", got.Hours)
	}
}

func TestStatusReminderRoundTripsThroughYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"

	enabled := true
	turns := 5
	hours := 1
	if err := Write(path, &Config{
		Reminders: &RemindersConfig{
			Status: &StatusReminderConfig{Enabled: &enabled, Turns: &turns, Hours: &hours},
		},
	}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	readCfg, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	got := readCfg.StatusReminder()
	if !got.Enabled || got.Turns != 5 || got.Hours != 1 {
		t.Errorf("round trip lost values: %+v", got)
	}
}
