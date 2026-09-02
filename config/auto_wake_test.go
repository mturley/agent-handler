package config

import "testing"

func TestAutoWakeOnRateLimitDefaultsEnabled(t *testing.T) {
	c := &Config{}
	if !c.AutoWakeOnRateLimit() {
		t.Error("expected auto wake to default to enabled")
	}
}

func TestAutoWakeOnRateLimitRespectsExplicitFalse(t *testing.T) {
	no := false
	c := &Config{AutoWake: &AutoWakeConfig{Enabled: &no}}
	if c.AutoWakeOnRateLimit() {
		t.Error("expected auto wake to be disabled when explicitly false")
	}
}

func TestAutoWakeOnRateLimitRespectsExplicitTrue(t *testing.T) {
	yes := true
	c := &Config{AutoWake: &AutoWakeConfig{Enabled: &yes}}
	if !c.AutoWakeOnRateLimit() {
		t.Error("expected auto wake to be enabled when explicitly true")
	}
}

// A present-but-empty block must not disable the feature.
func TestAutoWakeOnRateLimitWithEmptyBlockDefaultsEnabled(t *testing.T) {
	c := &Config{AutoWake: &AutoWakeConfig{}}
	if !c.AutoWakeOnRateLimit() {
		t.Error("expected an empty auto_wake block to leave the feature enabled")
	}
}

func TestAutoWakeThresholdDefaultsTo90(t *testing.T) {
	c := &Config{}
	if got := c.AutoWakeThresholdPercent(); got != 90 {
		t.Errorf("expected default threshold 90, got %d", got)
	}
}

func TestAutoWakeThresholdRespectsOverride(t *testing.T) {
	v := 75
	c := &Config{AutoWake: &AutoWakeConfig{ThresholdPercent: &v}}
	if got := c.AutoWakeThresholdPercent(); got != 75 {
		t.Errorf("expected threshold 75, got %d", got)
	}
}

// An out-of-range threshold would either never fire or fire constantly; fall
// back to the default rather than honouring nonsense.
func TestAutoWakeThresholdIgnoresOutOfRangeValues(t *testing.T) {
	for _, bad := range []int{0, -5, 101, 1000} {
		v := bad
		c := &Config{AutoWake: &AutoWakeConfig{ThresholdPercent: &v}}
		if got := c.AutoWakeThresholdPercent(); got != 90 {
			t.Errorf("threshold %d: expected fallback to 90, got %d", bad, got)
		}
	}
}

func TestAutoWakeConfigRoundTripsThroughYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	no := false
	v := 80
	if err := Write(path, &Config{AutoWake: &AutoWakeConfig{Enabled: &no, ThresholdPercent: &v}}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if got.AutoWakeOnRateLimit() {
		t.Error("expected disabled after round trip")
	}
	if got.AutoWakeThresholdPercent() != 80 {
		t.Errorf("expected threshold 80 after round trip, got %d", got.AutoWakeThresholdPercent())
	}
}
