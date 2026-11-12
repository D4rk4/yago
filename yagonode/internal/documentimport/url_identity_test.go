package documentimport

import (
	"net/url"
	"strings"
	"testing"
)

func TestValidateURLIdentityPreservesOnlyMarkedLegacyYaCyHost(t *testing.T) {
	legacy := "https://bad%20host.example.test/page"
	assertURLParseRejects(t, legacy)
	if !IsPreservableLegacyURL(legacy) {
		t.Fatal("known percent-encoded whitespace host was not recognized")
	}
	if err := ValidateURLIdentity(legacy, ""); err == nil {
		t.Fatal("unmarked malformed URL identity was accepted")
	}
	if err := ValidateURLIdentity(legacy, LegacyURLStatusEncodedHostWhitespace); err != nil {
		t.Fatalf("marked legacy identity was rejected: %v", err)
	}
	if err := ValidateURLIdentity("https://example.test/path", ""); err != nil {
		t.Fatalf("ordinary URL identity was rejected: %v", err)
	}
}

func TestValidateURLIdentityEnforcesRollbackSafeLimit(t *testing.T) {
	prefix := "https://archive.example.test/"
	atLimit := prefix + strings.Repeat("a", MaximumMigrationURLBytes-len(prefix))
	if len(atLimit) != MaximumMigrationURLBytes {
		t.Fatalf("boundary URL length = %d", len(atLimit))
	}
	if err := ValidateURLIdentity(atLimit, ""); err != nil {
		t.Fatalf("URL at rollback-safe limit was rejected: %v", err)
	}
	tooLong := atLimit + "a"
	if IsPreservableLegacyURL(tooLong) {
		t.Fatal("URL beyond the rollback-safe limit was preservable")
	}
	for _, marker := range []string{"", LegacyURLStatusInvalidEscape} {
		if err := ValidateURLIdentity(tooLong, marker); err == nil {
			t.Fatalf("over-limit URL with marker %q was accepted", marker)
		}
	}
	credentialPrefix := "https://user:secret@archive.example.test/"
	credentialURL := credentialPrefix + strings.Repeat(
		"a",
		MaximumMigrationURLBytes-len(credentialPrefix),
	)
	if IsPreservableLegacyURL(credentialURL) {
		t.Fatal("credential-bearing long URL was preservable")
	}
}

func TestValidateURLIdentityPreservesInvalidHostCharacterOnlyWhenMarked(t *testing.T) {
	legacy := "https://bad host.example.test/page"
	assertURLParseRejects(t, legacy)
	if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidHostCharacter {
		t.Fatalf("legacy status = %q, want invalid host character", got)
	}
	if err := ValidateURLIdentity(legacy, ""); err == nil {
		t.Fatal("unmarked invalid-host identity was accepted")
	}
	if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostCharacter); err != nil {
		t.Fatalf("marked invalid-host identity was rejected: %v", err)
	}
}

func TestValidateURLIdentityPreservesControlCharacterOnlyAsOpaque(t *testing.T) {
	legacy := "https://archive.example.test/path/\nrecord"
	assertURLParseRejects(t, legacy)
	status := PreservableLegacyURLStatus(legacy)
	if status != LegacyURLStatusControlCharacter {
		t.Fatalf("legacy status = %q, want control-character marker", status)
	}
	if err := ValidateURLIdentity(legacy, ""); err == nil {
		t.Fatal("unmarked control-character URL identity was accepted")
	}
	if err := ValidateURLIdentity(legacy, status); err != nil {
		t.Fatalf("marked opaque URL identity was rejected: %v", err)
	}
	if IsPreservableLegacyURL("https://user:secret@archive.example.test/path/\nrecord") {
		t.Fatal("credential-bearing control-character URL was preservable")
	}
	tooLong := "https://archive.example.test/\n" +
		strings.Repeat("x", MaximumMigrationURLBytes)
	if IsPreservableLegacyURL(tooLong) {
		t.Fatal("over-limit control-character URL was preservable")
	}
}

func TestValidateURLIdentityPreservesMalformedHostWithFragmentOrBackslash(t *testing.T) {
	for _, legacy := range []string{
		`https://bad\host.example.test/path`,
		"https://bad host.example.test/path#legacy-fragment",
	} {
		t.Run(legacy, func(t *testing.T) {
			if _, err := url.Parse(legacy); err == nil {
				t.Fatal("fixture should not be parseable")
			}
			if got := PreservableLegacyURLStatus(
				legacy,
			); got != LegacyURLStatusInvalidHostCharacter {
				t.Fatalf("legacy status = %q, want invalid host character", got)
			}
			if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostCharacter); err != nil {
				t.Fatalf("marked opaque identity was rejected: %v", err)
			}
		})
	}
	for _, unsafe := range []string{
		`https://user:secret@bad\host.example.test/path`,
		"https://bad%40host.example.test/path#fragment",
		"https://bad host.example.test/path\n",
	} {
		if IsPreservableLegacyURL(unsafe) {
			t.Fatalf("unsafe malformed identity was preserved: %q", unsafe)
		}
	}
}

func TestValidateURLIdentityPreservesMalformedSupportedProtocolHosts(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		rawURL string
		status string
	}{
		{
			name:   "ftp invalid host character",
			rawURL: "ftp://legacy host.example.test/archive.zip",
			status: LegacyURLStatusInvalidHostCharacter,
		},
		{
			name:   "smb invalid host character",
			rawURL: "smb://legacy host.example.test/share/document.pdf",
			status: LegacyURLStatusInvalidHostCharacter,
		},
		{
			name:   "ftp encoded host whitespace",
			rawURL: "ftp://legacy%20host.example.test/archive.zip",
			status: LegacyURLStatusEncodedHostWhitespace,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			status := PreservableLegacyURLStatus(fixture.rawURL)
			if status != fixture.status {
				t.Fatalf("legacy status = %q, want %q", status, fixture.status)
			}
			if err := ValidateURLIdentity(fixture.rawURL, status); err != nil {
				t.Fatalf("marked legacy identity was rejected: %v", err)
			}
		})
	}
}

func TestValidateURLIdentityPreservesMalformedPercentEscapeInLegacyHost(t *testing.T) {
	for _, legacy := range []string{
		"https://archive%zz.example.test/page?view=full%20text",
		"https://bad%zzhost.example.test/a%zzb",
		"https://bad%zzhost.example.test:8080/path",
	} {
		t.Run(legacy, func(t *testing.T) {
			if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidHostEscape {
				t.Fatalf("legacy status = %q, want malformed percent escape in host", got)
			}
			if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostEscape); err != nil {
				t.Fatalf("marked malformed-host identity was rejected: %v", err)
			}
			if err := ValidateURLIdentity(legacy, ""); err == nil {
				t.Fatal("unmarked malformed-host identity was accepted")
			}
		})
	}
}

func TestValidateURLIdentityPreservesMalformedPercentEscapeInHostWithPort(t *testing.T) {
	legacy := "https://archive%zz.example.test:443/path"
	if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidHostEscape {
		t.Fatalf("legacy status = %q, want malformed percent escape in host", got)
	}
	if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostEscape); err != nil {
		t.Fatalf("marked malformed-host identity was rejected: %v", err)
	}
	if PreservableLegacyURLStatus("https://user:secret@archive%zz.example.test:443/path") != "" {
		t.Fatal("credential-bearing malformed host was accepted without redaction")
	}
}

func TestValidateURLIdentityPreservesPercentEncodedLegacyHost(t *testing.T) {
	legacy := "https://bad%2fhost.example.test/path"
	assertURLParseRejects(t, legacy)
	if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidHostEscape {
		t.Fatalf("legacy status = %q, want invalid host escape", got)
	}
	if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostEscape); err != nil {
		t.Fatalf("marked opaque host identity was rejected: %v", err)
	}
}

func TestValidateURLIdentityPreservesPercentEncodedAtHostAsOpaque(t *testing.T) {
	legacy := "https://bad%40host.example.test/path"
	assertURLParseRejects(t, legacy)
	if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidHostEscape {
		t.Fatalf("legacy status = %q, want invalid host escape", got)
	}
	if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidHostEscape); err != nil {
		t.Fatalf("marked opaque host identity was rejected: %v", err)
	}
	if IsPreservableLegacyURL("https://user%40name@example.test/path") {
		t.Fatal("URL with an actual user-info delimiter was accepted")
	}
}

func assertURLParseRejects(t *testing.T, rawURL string) {
	t.Helper()
	if _, err := url.Parse(rawURL); err == nil {
		t.Fatal("fixture should be rejected by net/url")
	}
}

func TestValidateURLIdentityPreservesRejectedAuthorityAsOpaque(t *testing.T) {
	for _, rawURL := range []string{
		"https://bad%20host.example.test:bad/path",
		"https://[broken.example.test/path",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, err := url.Parse(rawURL); err == nil {
				t.Fatal("fixture should be rejected by net/url")
			}
			status := PreservableLegacyURLStatus(rawURL)
			if status != LegacyURLStatusInvalidAuthority {
				t.Fatalf("legacy status = %q, want invalid source authority", status)
			}
			if err := ValidateURLIdentity(rawURL, status); err != nil {
				t.Fatalf("marked opaque authority was rejected: %v", err)
			}
		})
	}
	for _, rawURL := range []string{
		"https://user:secret@bad%20host.example.test:bad/path",
		"https://bad%20host.example.test/path#fragment",
		"file:///private/document",
	} {
		if IsPreservableLegacyURL(rawURL) {
			t.Fatalf("unsafe or unsupported identity was accepted: %q", rawURL)
		}
	}
}

func TestValidateURLIdentityPreservesMalformedPercentEscapeInPathOrQuery(t *testing.T) {
	for _, legacy := range []string{
		"https://archive.example.test/path%zz/file",
		"https://archive.example.test?view=%",
	} {
		t.Run(legacy, func(t *testing.T) {
			if got := PreservableLegacyURLStatus(legacy); got != LegacyURLStatusInvalidEscape {
				t.Fatalf("legacy status = %q, want malformed percent escape in URL", got)
			}
			if err := ValidateURLIdentity(legacy, LegacyURLStatusInvalidEscape); err != nil {
				t.Fatalf("marked malformed URL identity was rejected: %v", err)
			}
		})
	}
}

func TestIsPreservableLegacyURLRejectsOtherMalformedAuthorityForms(t *testing.T) {
	for _, raw := range []string{
		"https://user%20name@example.test/path",
		"https://user:secret@archive.example.test/path%zz",
		"https://archive.example.test/path%zz#fragment",
		"https://archive.example.test/path%zz\\escape",
		"https://bad%20host.example.test/path#fragment",
		"https://badhost.example.test/a%20b",
		"https://user:password@bad host.example.test/path",
	} {
		t.Run(raw, func(t *testing.T) {
			if IsPreservableLegacyURL(raw) {
				t.Fatal("unrelated malformed URL was accepted as a legacy identity")
			}
		})
	}
}
