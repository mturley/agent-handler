package config

// Default thresholds for the status-emit reminder.
const (
	DefaultStatusReminderTurns = 10
	DefaultStatusReminderHours = 3
)

// RemindersConfig groups nudges the handler injects into sessions.
type RemindersConfig struct {
	Status *StatusReminderConfig `yaml:"status,omitempty"`
}

// StatusReminderConfig configures the reminder that fires when a session has
// gone too long without emitting a status event. All fields are pointers so an
// omitted key falls back to its default rather than to the zero value.
type StatusReminderConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	Turns   *int  `yaml:"turns,omitempty"`
	Hours   *int  `yaml:"hours,omitempty"`
}

// StatusReminderSettings is the resolved form, with defaults applied.
// Turns or Hours of 0 disables that individual check.
type StatusReminderSettings struct {
	Enabled bool
	Turns   int
	Hours   int
}

// StatusReminder resolves the status reminder settings, applying defaults for
// anything the user hasn't configured.
func (c *Config) StatusReminder() StatusReminderSettings {
	s := StatusReminderSettings{
		Enabled: true,
		Turns:   DefaultStatusReminderTurns,
		Hours:   DefaultStatusReminderHours,
	}
	if c == nil || c.Reminders == nil || c.Reminders.Status == nil {
		return s
	}
	rc := c.Reminders.Status
	if rc.Enabled != nil {
		s.Enabled = *rc.Enabled
	}
	if rc.Turns != nil {
		s.Turns = *rc.Turns
	}
	if rc.Hours != nil {
		s.Hours = *rc.Hours
	}
	return s
}
