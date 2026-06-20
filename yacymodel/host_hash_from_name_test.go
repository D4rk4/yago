package yacymodel

import "testing"

func TestHostHashFromNameMatchesURLHostHash(t *testing.T) {
	got := HostHashFromName("example.com")
	want := URLHash("http://example.com/").HostHash()
	if got != want {
		t.Fatalf("host hash = %q, want %q", got, want)
	}
}

func TestHostHashFromNameNormalizesCaseAndDots(t *testing.T) {
	want := HostHashFromName("example.com")
	for _, in := range []string{"Example.COM", ".example.com.", "EXAMPLE.com"} {
		if got := HostHashFromName(in); got != want {
			t.Errorf("host hash(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHostHashFromNameUsesFtpSchemeForFtpHosts(t *testing.T) {
	got := HostHashFromName("ftp.example.com")
	want := URLHash("ftp://ftp.example.com/").HostHash()
	if got != want {
		t.Fatalf("ftp host hash = %q, want %q", got, want)
	}
}
