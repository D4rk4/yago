package yagonode

import (
	"testing"
)

func TestStorageQuotaValueMatchesBootstrapRange(t *testing.T) {
	for value, expected := range map[string]string{
		"0B":                   "0B",
		"1B":                   "1B",
		"1024B":                "1KB",
		"9223372036854775807B": "9223372036854775807B",
	} {
		got, err := normalizeStorageQuota(value)
		if err != nil || got != expected {
			t.Fatalf("storage quota %q = %q, %v, want %q", value, got, err, expected)
		}
	}
	for _, value := range []string{"", "-1B", "9223372036854775808B"} {
		if _, err := normalizeStorageQuota(value); err == nil {
			t.Fatalf("storage quota %q must fail", value)
		}
	}
}
