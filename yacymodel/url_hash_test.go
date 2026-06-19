package yacymodel

import "testing"

func TestURLHashIsValidTwelveChar(t *testing.T) {
	h := URLHash("http://example.com/path?q=1")
	if len(h) != HashLength {
		t.Fatalf("URLHash length = %d, want %d", len(h), HashLength)
	}
	if !h.Valid() {
		t.Errorf("URLHash produced invalid hash %q", h)
	}
}

func TestURLHashDeterministic(t *testing.T) {
	first := URLHash("http://example.com/")
	second := URLHash("http://example.com/")
	if first != second {
		t.Error("URLHash must be deterministic")
	}
}

func TestURLHashSharesHostHashAcrossPaths(t *testing.T) {
	a := URLHash("http://example.com/one")
	b := URLHash("http://example.com/two")
	if a == b {
		t.Error("different paths must yield different url hashes")
	}
	if a.HostHash() != b.HostHash() {
		t.Errorf("same host must share host hash: %q vs %q", a.HostHash(), b.HostHash())
	}
}

func TestURLHashHostHashIsLastSixChars(t *testing.T) {
	h := URLHash("http://example.com/path")
	if h.HostHash() != string(h)[6:] {
		t.Errorf("HostHash = %q, want %q", h.HostHash(), string(h)[6:])
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
		h := URLHash(c.url)
		got := string(h)[11]
		want := Alphabet[c.flag]
		if got != want {
			t.Errorf("%s flag char = %q, want %q (flag %d)", c.url, got, want, c.flag)
		}
	}
}

func TestURLHashSubdomainChangesLocalAndHostHash(t *testing.T) {
	bare := URLHash("http://example.com/")
	sub := URLHash("http://www.example.com/")
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
