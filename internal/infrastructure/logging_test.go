package infrastructure

import "testing"

func TestConfigureLogging(t *testing.T) {
	for _, level := range []string{"", "debug", "INFO", "warn", "ERROR"} {
		if err := ConfigureLogging(func(string) string { return level }); err != nil {
			t.Errorf("ConfigureLogging(%q): %v", level, err)
		}
	}
}

func TestConfigureLoggingRejectsInvalidLevel(t *testing.T) {
	if err := ConfigureLogging(func(string) string { return "loud" }); err == nil {
		t.Fatal("expected error for invalid level")
	}
}
