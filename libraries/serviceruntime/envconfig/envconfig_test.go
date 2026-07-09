package envconfig_test

import (
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/serviceruntime/envconfig"
)

func fixedEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestString(t *testing.T) {
	getenv := fixedEnv(map[string]string{"SET": " value ", "BLANK": "  "})
	if got := envconfig.String(getenv, "SET", "fallback"); got != "value" {
		t.Errorf("SET = %q, want %q", got, "value")
	}
	if got := envconfig.String(getenv, "BLANK", "fallback"); got != "fallback" {
		t.Errorf("BLANK = %q, want fallback", got)
	}
	if got := envconfig.String(getenv, "MISSING", "fallback"); got != "fallback" {
		t.Errorf("MISSING = %q, want fallback", got)
	}
}

func TestPositiveInt(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "7", "ZERO": "0", "BAD": "x"})
	if got, err := envconfig.PositiveInt(getenv, "OK", 1); err != nil || got != 7 {
		t.Errorf("OK = %d, %v", got, err)
	}
	if got, err := envconfig.PositiveInt(getenv, "MISSING", 3); err != nil || got != 3 {
		t.Errorf("MISSING = %d, %v", got, err)
	}
	if _, err := envconfig.PositiveInt(getenv, "ZERO", 1); err == nil {
		t.Error("ZERO: expected error")
	}
	if _, err := envconfig.PositiveInt(getenv, "BAD", 1); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestPositiveInt64(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "1024", "NEG": "-1", "BAD": "x"})
	if got, err := envconfig.PositiveInt64(getenv, "OK", 1); err != nil || got != 1024 {
		t.Errorf("OK = %d, %v", got, err)
	}
	if got, err := envconfig.PositiveInt64(getenv, "MISSING", 5); err != nil || got != 5 {
		t.Errorf("MISSING = %d, %v", got, err)
	}
	if _, err := envconfig.PositiveInt64(getenv, "NEG", 1); err == nil {
		t.Error("NEG: expected error")
	}
	if _, err := envconfig.PositiveInt64(getenv, "BAD", 1); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestDuration(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "30s", "ZERO": "0s", "BAD": "x"})
	if got, err := envconfig.Duration(
		getenv,
		"OK",
		time.Second,
	); err != nil ||
		got != 30*time.Second {
		t.Errorf("OK = %v, %v", got, err)
	}
	if got, err := envconfig.Duration(
		getenv,
		"MISSING",
		time.Minute,
	); err != nil ||
		got != time.Minute {
		t.Errorf("MISSING = %v, %v", got, err)
	}
	if _, err := envconfig.Duration(getenv, "ZERO", time.Second); err == nil {
		t.Error("ZERO: expected error")
	}
	if _, err := envconfig.Duration(getenv, "BAD", time.Second); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestNonNegativeDuration(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "0s", "POS": "5s", "NEG": "-1s", "BAD": "x"})
	if got, err := envconfig.NonNegativeDuration(getenv, "OK"); err != nil || got != 0 {
		t.Errorf("OK = %v, %v", got, err)
	}
	if got, err := envconfig.NonNegativeDuration(
		getenv,
		"POS",
	); err != nil ||
		got != 5*time.Second {
		t.Errorf("POS = %v, %v", got, err)
	}
	if got, err := envconfig.NonNegativeDuration(getenv, "MISSING"); err != nil || got != 0 {
		t.Errorf("MISSING = %v, %v", got, err)
	}
	if _, err := envconfig.NonNegativeDuration(getenv, "NEG"); err == nil {
		t.Error("NEG: expected error")
	}
	if _, err := envconfig.NonNegativeDuration(getenv, "BAD"); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestInt(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "-4", "BAD": "x"})
	if got, err := envconfig.Int(getenv, "OK", 1); err != nil || got != -4 {
		t.Errorf("OK = %d, %v", got, err)
	}
	if got, err := envconfig.Int(getenv, "MISSING", 9); err != nil || got != 9 {
		t.Errorf("MISSING = %d, %v", got, err)
	}
	if _, err := envconfig.Int(getenv, "BAD", 1); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestNonNegativeInt(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "0", "NEG": "-1", "BAD": "x"})
	if got, err := envconfig.NonNegativeInt(getenv, "OK", 1); err != nil || got != 0 {
		t.Errorf("OK = %d, %v", got, err)
	}
	if got, err := envconfig.NonNegativeInt(getenv, "MISSING", 2); err != nil || got != 2 {
		t.Errorf("MISSING = %d, %v", got, err)
	}
	if _, err := envconfig.NonNegativeInt(getenv, "NEG", 1); err == nil {
		t.Error("NEG: expected error")
	}
	if _, err := envconfig.NonNegativeInt(getenv, "BAD", 1); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestBool(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": "true", "BAD": "maybe"})
	if got, err := envconfig.Bool(getenv, "OK", false); err != nil || !got {
		t.Errorf("OK = %v, %v", got, err)
	}
	if got, err := envconfig.Bool(getenv, "MISSING", true); err != nil || !got {
		t.Errorf("MISSING = %v, %v", got, err)
	}
	if _, err := envconfig.Bool(getenv, "BAD", false); err == nil {
		t.Error("BAD: expected error")
	}
}

func TestList(t *testing.T) {
	getenv := fixedEnv(map[string]string{"OK": " a , ,b ,c ", "BLANK": "  "})
	got := envconfig.List(getenv, "OK")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("OK = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("OK[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if envconfig.List(getenv, "BLANK") != nil {
		t.Error("BLANK: want nil")
	}
	if envconfig.List(getenv, "MISSING") != nil {
		t.Error("MISSING: want nil")
	}
}
