package cmd

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiRe = regexp.MustCompile("\033\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestUsageBar(t *testing.T) {
	cases := []struct {
		pct       int
		width     int
		wantBar   string
		wantColor string
	}{
		{0, 10, "░░░░░░░░░░", colorGreen},
		{21, 10, "▓▓░░░░░░░░", colorGreen},
		{49, 10, "▓▓▓▓░░░░░░", colorGreen},
		{50, 10, "▓▓▓▓▓░░░░░", colorYellow},
		{78, 10, "▓▓▓▓▓▓▓░░░", colorYellow},
		{80, 10, "▓▓▓▓▓▓▓▓░░", colorRed},
		{100, 10, "▓▓▓▓▓▓▓▓▓▓", colorRed},
		{60, 20, "▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░", colorYellow},
		// Out-of-range values are clamped rather than producing a negative repeat.
		{-5, 10, "░░░░░░░░░░", colorGreen},
		{140, 10, "▓▓▓▓▓▓▓▓▓▓", colorRed},
	}
	for _, c := range cases {
		bar, color := usageBar(c.pct, c.width)
		if bar != c.wantBar {
			t.Errorf("usageBar(%d, %d) bar = %q, want %q", c.pct, c.width, bar, c.wantBar)
		}
		if color != c.wantColor {
			t.Errorf("usageBar(%d, %d) color = %q, want %q", c.pct, c.width, color, c.wantColor)
		}
		if got := len([]rune(bar)); got != c.width {
			t.Errorf("usageBar(%d, %d) length = %d, want %d", c.pct, c.width, got, c.width)
		}
	}
}

func TestFormatResetTime(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)

	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"later today", now.Add(4 * time.Hour).Truncate(time.Minute), "8:08pm"},
		{"just under a day", now.Add(23 * time.Hour), "3:08pm"},
		{"just over a day", now.Add(25 * time.Hour), "Thu"},
		{"next week", now.Add(6 * 24 * time.Hour), "Tue"},
	}
	for _, c := range cases {
		if got := formatResetTime(c.at.Unix(), now); got != c.want {
			t.Errorf("%s: formatResetTime = %q, want %q", c.name, got, c.want)
		}
	}

	// A missing timestamp yields no suffix rather than the epoch.
	if got := formatResetTime(0, now); got != "" {
		t.Errorf("formatResetTime(0) = %q, want empty", got)
	}
	if got := formatResetTime(-1, now); got != "" {
		t.Errorf("formatResetTime(-1) = %q, want empty", got)
	}
}

func TestFormatRateLimitsLineOmittedWithoutData(t *testing.T) {
	// Vertex/Bedrock sessions omit rate_limits entirely — no line at all,
	// which must not be confused with a session sitting at 0%.
	if _, ok := formatRateLimitsLine(&hookInput{}, time.Now()); ok {
		t.Error("expected no line when RateLimits is nil")
	}
	if _, ok := formatRateLimitsLine(nil, time.Now()); ok {
		t.Error("expected no line for nil input")
	}
}

func TestFormatRateLimitsLine(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)
	in := &hookInput{}
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 21, ResetsAt: now.Add(4 * time.Hour).Unix()},
		SevenDay: rateLimitWindow{UsedPercentage: 12, ResetsAt: now.Add(6 * 24 * time.Hour).Unix()},
	}

	line, ok := formatRateLimitsLine(in, now)
	if !ok {
		t.Fatal("expected a line when RateLimits is present")
	}

	got := stripANSI(line)
	want := "Limits: 5h ▓▓░░░░░░░░ 21% ·8:08pm  7d ▓░░░░░░░░░ 12% ·Tue"
	if got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestFormatRateLimitsLineColorsHotWindow(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)
	in := &hookInput{}
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 92, ResetsAt: now.Add(time.Hour).Unix()},
		SevenDay: rateLimitWindow{UsedPercentage: 12, ResetsAt: now.Add(6 * 24 * time.Hour).Unix()},
	}

	line, ok := formatRateLimitsLine(in, now)
	if !ok {
		t.Fatal("expected a line when RateLimits is present")
	}
	// The saturated 5h window turns red while the quiet 7d window stays green.
	if !strings.Contains(line, colorRed) {
		t.Error("expected the 92% window to render red")
	}
	if !strings.Contains(line, colorGreen) {
		t.Error("expected the 12% window to render green")
	}
}

func TestHookInputParsesRateLimits(t *testing.T) {
	// Shape taken verbatim from a captured first-party statusline payload.
	raw := `{"session_id":"abc","rate_limits":{"five_hour":{"used_percentage":21,"resets_at":1787790000},"seven_day":{"used_percentage":12,"resets_at":1788296400}}}`
	in, err := parseHookInputForTest(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if in.RateLimits == nil {
		t.Fatal("RateLimits was not parsed")
	}
	if in.RateLimits.FiveHour.UsedPercentage != 21 || in.RateLimits.FiveHour.ResetsAt != 1787790000 {
		t.Errorf("five_hour = %+v", in.RateLimits.FiveHour)
	}
	if in.RateLimits.SevenDay.UsedPercentage != 12 || in.RateLimits.SevenDay.ResetsAt != 1788296400 {
		t.Errorf("seven_day = %+v", in.RateLimits.SevenDay)
	}

	// A payload from a non-first-party backend leaves the pointer nil.
	in, err = parseHookInputForTest(`{"session_id":"abc"}`)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if in.RateLimits != nil {
		t.Errorf("RateLimits = %+v, want nil", in.RateLimits)
	}
}

func parseHookInputForTest(raw string) (*hookInput, error) {
	var in hookInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, err
	}
	return &in, nil
}
