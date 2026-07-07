package yagocrawlcontract

import (
	"testing"
	"time"
)

func TestParseRecrawlIntervalAcceptsOffSentinels(t *testing.T) {
	for _, raw := range []string{"", "   ", "0", "off", "OFF", "None", "disabled"} {
		got, err := ParseRecrawlInterval(raw)
		if err != nil {
			t.Fatalf("ParseRecrawlInterval(%q) error = %v", raw, err)
		}
		if got != 0 {
			t.Fatalf("ParseRecrawlInterval(%q) = %v, want 0", raw, got)
		}
	}
}

func TestParseRecrawlIntervalAcceptsDurationsAndCalendarUnits(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"720h", 720 * time.Hour},
		{"90m", 90 * time.Minute},
		{"1h", time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"  30d  ", 30 * 24 * time.Hour},
		{"365d", 365 * 24 * time.Hour},
	}
	for _, tc := range cases {
		got, err := ParseRecrawlInterval(tc.raw)
		if err != nil {
			t.Fatalf("ParseRecrawlInterval(%q) error = %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseRecrawlInterval(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestParseRecrawlIntervalRejectsBadValues(t *testing.T) {
	for _, raw := range []string{
		"-1h",      // negative duration
		"-5d",      // negative calendar count
		"30m",      // below the 1h floor
		"400d",     // above the 365d ceiling
		"366d",     // one day above the ceiling
		"nonsense", // not a duration at all
		"30",       // bare integer without a unit
		"1.5d",     // non-integer calendar count
		"100000w",  // overflows time.Duration
	} {
		if _, err := ParseRecrawlInterval(raw); err == nil {
			t.Fatalf("ParseRecrawlInterval(%q) accepted an invalid value", raw)
		}
	}
}

func TestFormatRecrawlInterval(t *testing.T) {
	cases := map[time.Duration]string{
		0:                   "off",
		-time.Hour:          "off",
		7 * 24 * time.Hour:  "1w",
		14 * 24 * time.Hour: "2w",
		30 * 24 * time.Hour: "30d",
		24 * time.Hour:      "1d",
		90 * time.Minute:    "1h30m0s",
	}
	for d, want := range cases {
		if got := FormatRecrawlInterval(d); got != want {
			t.Fatalf("FormatRecrawlInterval(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestRecrawlCalendarUnitGuardsEmpty covers the defensive empty-string guard
// the public parser never reaches (it resolves "" to off first).
func TestRecrawlCalendarUnitGuardsEmpty(t *testing.T) {
	if unit, ok := recrawlCalendarUnit(""); ok || unit != 0 {
		t.Fatalf("recrawlCalendarUnit(%q) = %v %v, want 0 false", "", unit, ok)
	}
}

func TestRecrawlIntervalDefaultRoundTrips(t *testing.T) {
	formatted := FormatRecrawlInterval(DefaultRecrawlInterval)
	if formatted != "30d" {
		t.Fatalf("FormatRecrawlInterval(default) = %q, want 30d", formatted)
	}
	parsed, err := ParseRecrawlInterval(formatted)
	if err != nil {
		t.Fatalf("ParseRecrawlInterval(%q) error = %v", formatted, err)
	}
	if parsed != DefaultRecrawlInterval {
		t.Fatalf("round-trip = %v, want %v", parsed, DefaultRecrawlInterval)
	}
}
