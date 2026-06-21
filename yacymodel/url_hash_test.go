package yacymodel

import "testing"

func TestURLHashIsValidTwelveChar(t *testing.T) {
	h, err := HashURL("http://example.com/path?q=1")
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	if len(h) != HashLength {
		t.Fatalf("URLHash length = %d, want %d", len(h), HashLength)
	}
	if _, err := ParseURLHash(string(h)); err != nil {
		t.Errorf("URLHash produced invalid hash %q: %v", h, err)
	}
}

func TestURLHashDeterministic(t *testing.T) {
	first, err := HashURL("http://example.com/")
	if err != nil {
		t.Fatalf("HashURL first: %v", err)
	}
	second, err := HashURL("http://example.com/")
	if err != nil {
		t.Fatalf("HashURL second: %v", err)
	}
	if first != second {
		t.Error("URLHash must be deterministic")
	}
}

func TestURLHashSharesHostHashAcrossPaths(t *testing.T) {
	a, err := HashURL("http://example.com/one")
	if err != nil {
		t.Fatalf("HashURL a: %v", err)
	}
	b, err := HashURL("http://example.com/two")
	if err != nil {
		t.Fatalf("HashURL b: %v", err)
	}
	if a == b {
		t.Error("different paths must yield different url hashes")
	}
	aHost, err := a.HostHash()
	if err != nil {
		t.Fatalf("HostHash(a): %v", err)
	}
	bHost, err := b.HostHash()
	if err != nil {
		t.Fatalf("HostHash(b): %v", err)
	}
	if aHost != bHost {
		t.Errorf("same host must share host hash: %q vs %q", aHost, bHost)
	}
}

func TestURLHashHostHashIsLastSixChars(t *testing.T) {
	h, err := HashURL("http://example.com/path")
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	hostHash, err := h.HostHash()
	if err != nil {
		t.Fatalf("HostHash: %v", err)
	}
	if hostHash != string(h)[6:] {
		t.Errorf("HostHash = %q, want %q", hostHash, string(h)[6:])
	}
}

func TestHashURLHostNormalizesCaseAndDots(t *testing.T) {
	want, err := HashURLHost("example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	for _, in := range []string{"Example.COM", ".example.com.", "EXAMPLE.com"} {
		got, err := HashURLHost(in)
		if err != nil {
			t.Fatalf("HashURLHost(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("HashURLHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashURLHostUsesFtpSchemeForFtpHosts(t *testing.T) {
	got, err := HashURLHost("ftp.example.com")
	if err != nil {
		t.Fatalf("HashURLHost: %v", err)
	}
	want, err := HashURL("ftp://ftp.example.com/")
	if err != nil {
		t.Fatalf("HashURL: %v", err)
	}
	if got != want {
		t.Fatalf("ftp host hash = %q, want %q", got, want)
	}
}

func TestURLHashFlagByteEncodesProtocolDomainAndLength(t *testing.T) {
	cases := []struct {
		url  string
		flag byte
	}{
		{"http://example.com/", byte(tldGenericID << 2)},
		{"https://example.com/", 32 | byte(tldGenericID<<2)},
		{"http://example.de/", byte(tldEuropeRussiaID << 2)},
		{"http://example.jp/", byte(tldSouthEastAsiaID << 2)},
		{"http://exampleab.com/", byte(tldGenericID<<2) | 1},
		{"http://longexampledomain.com/", byte(tldGenericID<<2) | 3},
	}
	for _, c := range cases {
		h, err := HashURL(c.url)
		if err != nil {
			t.Fatalf("HashURL(%q): %v", c.url, err)
		}
		got := string(h)[11]
		want := Alphabet[c.flag]
		if got != want {
			t.Errorf("%s flag char = %q, want %q (flag %d)", c.url, got, want, c.flag)
		}
	}
}

func TestURLHashSubdomainChangesLocalAndHostHash(t *testing.T) {
	bare, err := HashURL("http://example.com/")
	if err != nil {
		t.Fatalf("HashURL bare: %v", err)
	}
	sub, err := HashURL("http://www.example.com/")
	if err != nil {
		t.Fatalf("HashURL sub: %v", err)
	}
	if bare == sub {
		t.Error("subdomain must change the url hash")
	}
}

func TestNormalformOmitsDefaultPortAndLowercasesHost(t *testing.T) {
	cases := map[string]string{
		"http://Example.COM:80/Path":     "http://example.com/Path",
		"https://Example.com:443/":       "https://example.com/",
		"http://example.com:8080/x":      "http://example.com:8080/x",
		"http://example.com/a?sid=1&b=2": "http://example.com/a?b=2",
		"http://example.com/a?b=2&sid=1": "http://example.com/a?b=2",
		"http://example.com/a?sid=1":     "http://example.com/a",
	}
	for in, want := range cases {
		if got := parseURLAddress(in).normalform(); got != want {
			t.Errorf("normalform(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDomainID(t *testing.T) {
	cases := map[string]int{
		"example.com": tldGenericID,
		"example.de":  tldEuropeRussiaID,
		"example.jp":  tldSouthEastAsiaID,
		"example.br":  tldMiddleSouthAmericaID,
		"example.ir":  tldMiddleEastWestAsiaID,
		"example.us":  tldNorthAmericaOceaniaID,
		"example.za":  tldAfricaID,
		"localhost":   tldLocalID,
		"127.0.0.1":   tldLocalID,
		"192.168.0.1": tldLocalID,
		"singlelabel": tldLocalID,
	}
	for host, want := range cases {
		if got := DomainID(host); got != want {
			t.Errorf("DomainID(%q) = %d, want %d", host, got, want)
		}
	}
}

func TestRootpathExtraction(t *testing.T) {
	cases := map[string]string{
		"http://example.com/":        "",
		"http://example.com/foo/bar": "foo",
		"http://example.com/foo/":    "",
		"http://example.com/a/b/c":   "a",
	}
	for url, want := range cases {
		if got := parseURLAddress(url).rootpath(); got != want {
			t.Errorf("rootpath(%q) = %q, want %q", url, got, want)
		}
	}
}
