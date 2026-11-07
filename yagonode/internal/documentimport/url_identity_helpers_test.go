package documentimport

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLegacyURLIdentityHelpersClassifyAcceptedAndRejectedValues(t *testing.T) {
	rawWithFragment := "https://example.test/%zz#fragment"
	fragmentAuthority, fragmentParseErr, fragmentParsed := legacyAuthorityError(rawWithFragment)
	var fragmentEscape url.EscapeError
	if !fragmentParsed || !errors.As(fragmentParseErr.Err, &fragmentEscape) {
		t.Fatalf(
			"fragment malformed-escape parser result = %q, %v, %t",
			fragmentAuthority,
			fragmentParseErr,
			fragmentParsed,
		)
	}
	if encodedHostWhitespaceStatus(fragmentAuthority, fragmentEscape) != "" ||
		invalidHostPercentEscapeStatus(rawWithFragment, fragmentAuthority) != "" ||
		malformedPercentEscapeURLStatus(rawWithFragment, fragmentAuthority) != "" {
		t.Fatal("fragment malformed escape matched a supported preservation rule")
	}
	for _, raw := range []string{
		"file://bad%zz.example.test/path",
		"https://example.test/%zz#fragment",
	} {
		if got := PreservableLegacyURLStatus(raw); got != "" {
			t.Errorf("unpreservable malformed URL %q status = %q", raw, got)
		}
	}
	if got := controlCharacterIdentityStatus(
		"https://example.test/a\nb",
	); got != LegacyURLStatusControlCharacter {
		t.Fatalf("control-character status = %q", got)
	}
	for _, raw := range []string{
		"https://example.test/a#b\nc",
		"https://example.test/a\\b\nc",
		"file://example.test/a\nb",
		strings.Repeat("x", MaximumMigrationURLBytes+1) + "\n",
	} {
		if got := controlCharacterIdentityStatus(raw); got != "" {
			t.Errorf("control-character URL %q received status %q", raw, got)
		}
	}
}

func TestLegacyURLIdentityInvalidAuthorityPreservation(t *testing.T) {
	if got := preservableInvalidAuthorityStatus(
		"https://bad%20host.example.test:bad/path", "bad%20host.example.test:bad",
	); got != LegacyURLStatusInvalidAuthority {
		t.Fatalf("invalid authority status = %q", got)
	}
	for _, fixture := range [][2]string{
		{"https://user@bad.test/path", "user@bad.test"},
		{"https://bad%40host.test/path", "bad%40host.test"},
		{"https://bad.test/path#fragment", "bad.test"},
		{"https://bad.test/path\n", "bad.test"},
		{"file://bad.test/path", "bad.test"},
		{"https://bad.test/path", ""},
	} {
		if got := preservableInvalidAuthorityStatus(fixture[0], fixture[1]); got != "" {
			t.Errorf("unsafe authority %q received status %q", fixture[1], got)
		}
	}
}

func TestLegacyURLIdentityMalformedQueryAndPathEscapes(t *testing.T) {
	if got := malformedQueryPercentEscapeStatus(
		"https://example.test/?q=%zz",
	); got != LegacyURLStatusInvalidEscape {
		t.Fatalf("malformed query status = %q", got)
	}
	for _, raw := range []string{
		"https://example.test/?q=%20",
		"https://user@example.test/?q=%zz",
		"https://example.test/?q=%zz#fragment",
		"file://example.test/?q=%zz",
	} {
		if got := malformedQueryPercentEscapeStatus(raw); got != "" {
			t.Errorf("query URL %q received status %q", raw, got)
		}
	}

	if got := malformedPercentEscapeURLStatus(
		"https://example.test/path%zz", "example.test",
	); got != LegacyURLStatusInvalidEscape {
		t.Fatalf("malformed path status = %q", got)
	}
	for _, fixture := range [][2]string{
		{"https://example.test/path%20", "example.test"},
		{"https://example.test/path%zz", "example.test/longer"},
		{"https://example.test/path%zz#fragment", "example.test"},
		{"https://example.test/path%zz\\suffix", "example.test"},
		{"file://example.test/path%zz", "example.test"},
		{"relative/path%zz", "example.test"},
	} {
		if got := malformedPercentEscapeURLStatus(fixture[0], fixture[1]); got != "" {
			t.Errorf("malformed URL %q received status %q", fixture[0], got)
		}
	}
	if got := invalidHostPercentEscapeStatus("relative%zz", "bad%zz"); got != "" {
		t.Fatalf("relative host escape status = %q", got)
	}
}

func TestLegacyURLIdentityInvalidHostEscape(t *testing.T) {
	if got := invalidHostPercentEscapeStatus(
		"https://bad%zz.example.test/path", "bad%zz.example.test",
	); got != LegacyURLStatusInvalidHostEscape {
		t.Fatalf("host escape status = %q", got)
	}
	for _, fixture := range [][2]string{
		{"https://bad.example.test/path", "bad.example.test"},
		{"https://bad%zz.example.test/path\n", "bad%zz.example.test"},
		{"https://bad%zz.example.test/path#fragment", "bad%zz.example.test"},
		{"file://bad%zz.example.test/path", "bad%zz.example.test"},
		{"relative://bad%zz.example.test/path", "bad%zz.example.test"},
	} {
		if got := invalidHostPercentEscapeStatus(fixture[0], fixture[1]); got != "" {
			t.Errorf("host URL %q received status %q", fixture[0], got)
		}
	}
}

func TestLegacyURLIdentityEscapeAndAuthorityBoundaries(t *testing.T) {
	for _, fixture := range []struct {
		value string
		want  bool
	}{
		{value: "", want: false},
		{value: "ordinary", want: false},
		{value: "%", want: true},
		{value: "%z0", want: true},
		{value: "%2f", want: false},
		{value: "%2f%", want: true},
	} {
		if got := hasMalformedPercentEscape(fixture.value); got != fixture.want {
			t.Errorf(
				"hasMalformedPercentEscape(%q) = %t, want %t",
				fixture.value,
				got,
				fixture.want,
			)
		}
	}

	validAuthority, validParseErr, valid := legacyAuthorityError("https://example.test/path")
	if valid || validAuthority != "" || validParseErr != nil {
		t.Fatalf("valid URL authority result = %q, %v, %t", validAuthority, validParseErr, valid)
	}
	badAuthority, parseErr, ok := legacyAuthorityError("https://bad host.example.test/path")
	if !ok || badAuthority != "bad host.example.test" || parseErr == nil {
		t.Fatalf("malformed URL authority result = %q, %v, %t", badAuthority, parseErr, ok)
	}
	for _, raw := range []string{
		"",
		strings.Repeat("x", yagocrawlcontract.MaximumCrawlURLBytes+1),
		"https://example.test/path\n",
		"relative/path",
		"file://example.test/path",
		"https:///path",
		"https://user:secret@example.test/path",
	} {
		if authority, _, ok := legacyAuthorityError(raw); ok || authority != "" {
			t.Errorf("unsafe authority input %q was preserved", raw)
		}
	}

	escape := url.EscapeError("invalid URL escape %20")
	if got := encodedHostWhitespaceStatus(
		"bad%20host.example.test",
		escape,
	); got != LegacyURLStatusEncodedHostWhitespace {
		t.Fatalf("encoded whitespace status = %q", got)
	}
	for _, fixture := range []struct {
		authority string
		escape    url.EscapeError
	}{
		{authority: "bad.example.test", escape: escape},
		{authority: "bad%20host%zz.example.test", escape: escape},
		{authority: "bad%20_host.example.test", escape: escape},
		{authority: "bad%20host.example.test", escape: "invalid URL escape %2f"},
	} {
		if got := encodedHostWhitespaceStatus(fixture.authority, fixture.escape); got != "" {
			t.Errorf("unsafe encoded host %q received status %q", fixture.authority, got)
		}
	}
}
