package yagonode

import (
	"strings"
	"testing"
)

func TestPublicSelfTestURLNormalization(t *testing.T) {
	adminDefinition := indexSettingDefinitions()[settingKeyNetworkPublicSelfTest]
	cases := []struct {
		raw      string
		expected string
	}{
		{raw: "", expected: ""},
		{
			raw:      " HTTPS://Exämple.COM:443/a/../probe/ ",
			expected: "https://xn--exmple-cua.com/probe",
		},
		{raw: "http://[2001:0db8::1]:80/status", expected: "http://[2001:db8::1]/status"},
		{raw: "https://example.com:8443/base/", expected: "https://example.com:8443/base"},
	}
	for _, test := range cases {
		normalized, err := normalizeOptionalPublicSelfTestURL(test.raw)
		if err != nil {
			t.Fatalf("normalize %q: %v", test.raw, err)
		}
		if normalized != test.expected {
			t.Errorf("normalize %q = %q, want %q", test.raw, normalized, test.expected)
		}
		adminValue, adminErr := adminDefinition.normalize(test.raw)
		if adminErr != nil || adminValue != test.expected {
			t.Errorf(
				"Admin normalize %q = %q, %v, want %q",
				test.raw,
				adminValue,
				adminErr,
				test.expected,
			)
		}
		if test.raw == "" {
			continue
		}
		parsed, parseErr := publicSelfTestURL(
			envFrom(map[string]string{envPublicSelfTestURL: test.raw}),
			defaultPeerAddr,
		)
		if parseErr != nil {
			t.Fatalf("bootstrap %q: %v", test.raw, parseErr)
		}
		if parsed.String() != test.expected {
			t.Errorf("bootstrap %q = %q, want %q", test.raw, parsed, test.expected)
		}
	}
}

func TestPublicSelfTestURLRejectsUnsafeValues(t *testing.T) {
	adminDefinition := indexSettingDefinitions()[settingKeyNetworkPublicSelfTest]
	values := []string{
		"https://user:secret@example.com",
		"https://example.com/path?probe=1",
		"https://example.com/path#fragment",
		"https://example.com/path#",
		"https:example.com/path",
		"https://example.com/\npath",
		"file:///tmp/node",
		"https:///missing-host",
		"https://exam\u200bple.com",
		"https://\ufffd.example",
		"https://example.com:0",
		strings.Repeat("x", maximumPublicSelfTestURLBytes+1),
	}
	for _, raw := range values {
		if _, err := normalizeOptionalPublicSelfTestURL(raw); err == nil {
			t.Errorf("normalizer accepted %q", raw)
		}
		if _, err := adminDefinition.normalize(raw); err == nil {
			t.Errorf("Admin accepted %q", raw)
		}
		if _, err := publicSelfTestURL(
			envFrom(map[string]string{envPublicSelfTestURL: raw}),
			defaultPeerAddr,
		); err == nil {
			t.Errorf("bootstrap accepted %q", raw)
		}
	}
}
