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

func TestFormatRateLimitsOmittedWithoutData(t *testing.T) {
	// Vertex/Bedrock sessions omit rate_limits entirely — no line at all,
	// which must not be confused with a session sitting at 0%.
	if _, ok := formatRateLimits(&hookInput{}, time.Now()); ok {
		t.Error("expected no line when RateLimits is nil")
	}
	if _, ok := formatRateLimits(nil, time.Now()); ok {
		t.Error("expected no line for nil input")
	}
}

func TestFormatRateLimits(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)
	in := &hookInput{}
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 21, ResetsAt: float64(now.Add(4 * time.Hour).Unix())},
		SevenDay: rateLimitWindow{UsedPercentage: 12, ResetsAt: float64(now.Add(6 * 24 * time.Hour).Unix())},
	}

	line, ok := formatRateLimits(in, now)
	if !ok {
		t.Fatal("expected a segment when RateLimits is present")
	}

	got := stripANSI(line)
	want := "5h ▓▓░░░░░░░░ 21% ·8:08pm  7d ▓░░░░░░░░░ 12% ·Tue"
	if got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func TestFormatRateLimitsColorsHotWindow(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)
	in := &hookInput{}
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 92, ResetsAt: float64(now.Add(time.Hour).Unix())},
		SevenDay: rateLimitWindow{UsedPercentage: 12, ResetsAt: float64(now.Add(6 * 24 * time.Hour).Unix())},
	}

	line, ok := formatRateLimits(in, now)
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

// The API reports percentages as JSON numbers that are not always integral —
// a real payload carried 14.000000000000002. Decoding that into an int failed,
// and because a parse error aborts before any output, the affected sessions
// lost their entire statusline rather than just this line.
func TestFormatRateLimitsFractionalPercentages(t *testing.T) {
	raw := `{"session_id":"abc","rate_limits":{"five_hour":{"used_percentage":43,"resets_at":1787790000},"seven_day":{"used_percentage":14.000000000000002,"resets_at":1788296400}}}`
	in, err := parseHookInputForTest(raw)
	if err != nil {
		t.Fatalf("unmarshal of a real captured payload failed: %v", err)
	}
	if in.RateLimits == nil {
		t.Fatal("RateLimits was not parsed")
	}

	now := time.Unix(1787790000, 0).Add(-2 * time.Hour)
	line, ok := formatRateLimits(in, now)
	if !ok {
		t.Fatal("expected a line")
	}
	// 14.000000000000002 rounds to a clean 14%, not 14.000000000000002%.
	if got := stripANSI(line); !strings.Contains(got, "14%") {
		t.Errorf("line = %q, want it to contain \"14%%\"", got)
	}
}

func TestFormatRateLimitsRoundsHalfUp(t *testing.T) {
	now := time.Date(2026, 8, 26, 16, 8, 0, 0, time.Local)
	in := &hookInput{}
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 49.6, ResetsAt: float64(now.Add(time.Hour).Unix())},
		SevenDay: rateLimitWindow{UsedPercentage: 0.4, ResetsAt: float64(now.Add(6 * 24 * time.Hour).Unix())},
	}
	line, _ := formatRateLimits(in, now)
	got := stripANSI(line)
	if !strings.Contains(got, "50%") {
		t.Errorf("49.6 should display as 50%%, got %q", got)
	}
	if !strings.Contains(got, "0%") {
		t.Errorf("0.4 should display as 0%%, got %q", got)
	}
}

// The limits ride on the model line, trailing the cost after a dot separator.
func TestFormatModelLineAppendsRateLimits(t *testing.T) {
	in := &hookInput{}
	in.Model.DisplayName = "Opus 5"
	in.ContextWindow.UsedPercentage = 60
	in.RateLimits = &rateLimits{
		FiveHour: rateLimitWindow{UsedPercentage: 43, ResetsAt: float64(time.Now().Add(2 * time.Hour).Unix())},
		SevenDay: rateLimitWindow{UsedPercentage: 14.000000000000002, ResetsAt: float64(time.Now().Add(5 * 24 * time.Hour).Unix())},
	}

	got := stripANSI(formatModelLine(in, 12.34, 0))
	// The context bar halves to make room for the two limit bars.
	if !strings.Contains(got, strings.Repeat("▓", 6)+strings.Repeat("░", 4)+" 60% ctx") {
		t.Errorf("context bar should narrow to %d chars alongside limits, got %q", narrowContextBarWidth, got)
	}
	if !strings.Contains(got, "60% ctx · $12.34 · 5h ") {
		t.Errorf("limits should follow the cost after a dot separator, got %q", got)
	}
	if !strings.Contains(got, "14%") {
		t.Errorf("fractional percentage should round for display, got %q", got)
	}

	// Without rate limits the line ends at the cost, unchanged from before.
	in.RateLimits = nil
	got = stripANSI(formatModelLine(in, 12.34, 0))
	if strings.Contains(got, "5h ") || strings.Contains(got, "7d ") {
		t.Errorf("no limits segment expected, got %q", got)
	}
	if !strings.HasSuffix(got, "$12.34") {
		t.Errorf("line should end at the cost, got %q", got)
	}
	// With the line to itself, the context bar keeps its full width.
	if !strings.Contains(got, strings.Repeat("▓", 12)+strings.Repeat("░", 8)+" 60% ctx") {
		t.Errorf("context bar should stay %d chars without limits, got %q", contextBarWidth, got)
	}
}

func parseHookInputForTest(raw string) (*hookInput, error) {
	var in hookInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, err
	}
	return &in, nil
}
