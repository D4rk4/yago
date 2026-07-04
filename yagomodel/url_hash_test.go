package yagomodel

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

func TestYaCyDigestURLDefaultPortNormalformFixtures(t *testing.T) {
	cases := map[string]string{
		"http://www.yacy.net:":   "http://www.yacy.net/",
		"http://www.yacy.net:80": "http://www.yacy.net/",
		"http://www.yacy.net:/":  "http://www.yacy.net/",
		"http://www.yacy.net: /": "http://www.yacy.net/",
	}
	for raw, want := range cases {
		if got := parseURLAddress(raw).normalform(); got != want {
			t.Errorf("normalform(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestYaCyDigestURLHostHashFixtures(t *testing.T) {
	base := hostHashForURL(t, "http://example.test")

	sameHost := []string{
		"http://example.test/path/",
		"http://example.test/path/resource.html",
		"http://example.test/path/?a=b",
		"http://example.test/path/#fragment",
	}
	for _, raw := range sameHost {
		if got := hostHashForURL(t, raw); got != base {
			t.Errorf("host hash for %q = %q, want %q", raw, got, base)
		}
	}

	differentHostHash := []string{
		"https://example.test",
		"http://example.test:8080",
		"http://example.net",
	}
	for _, raw := range differentHostHash {
		if got := hostHashForURL(t, raw); got == base {
			t.Errorf("host hash for %q = %q, want different from %q", raw, got, base)
		}
	}
}

func TestYaCyDigestURLFileHashFixtures(t *testing.T) {
	want := hashForURL(t, "file:///C:/tmp/test.html")
	for _, raw := range []string{
		"file:///C:\\tmp\\test.html",
		"file:///C:/tmp\\test.html",
		"file:///C:\\tmp/test.html",
		"file:///C:/tmp/test.html",
		"file://C:/tmp/test.html",
		"file://C:\\tmp\\test.html",
		"file://C:tmp/test.html",
		"file://C:tmp\\test.html",
	} {
		if got := hashForURL(t, raw); got != want {
			t.Errorf(
				"HashURL(%q) = %q, want %q; normalform %q",
				raw,
				got,
				want,
				parseURLAddress(raw).normalform(),
			)
		}
	}
}

func TestYaCyMultiProtocolURLSessionIDRemovalFixtures(t *testing.T) {
	cases := map[string]string{
		"http://test.de/bla.php?phpsessionid=asdf":                 "http://test.de/bla.php",
		"http://test.de/bla?phpsessionid=asdf&fdsa=asdf":           "http://test.de/bla?fdsa=asdf",
		"http://test.de/bla?asdf=fdsa&phpsessionid=asdf":           "http://test.de/bla?asdf=fdsa",
		"http://test.de/bla?asdf=fdsa&phpsessionid=asdf&fdsa=asdf": "http://test.de/bla?asdf=fdsa&fdsa=asdf",
	}
	for raw, want := range cases {
		if got := parseURLAddress(raw).normalform(); got != want {
			t.Errorf("normalform(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestYaCyMultiProtocolURLBackpathFixtures(t *testing.T) {
	cases := map[string]string{
		"/..home":                   "/..home",
		"/test/..home/test.html":    "/test/..home/test.html",
		"/test/..":                  "/",
		"/test/../":                 "/",
		"/test/test2/..":            "/test",
		"/test/test2/../":           "/test/",
		"/test/test2/../hallo":      "/test/hallo",
		"/test/test2/../hallo/":     "/test/hallo/",
		"/home/..test/../hallo/../": "/home/",
		"/../":                      "/",
		"/..":                       "/",
		"/../../../image.jpg":       "/image.jpg",
	}
	for rawPath, wantPath := range cases {
		raw := "http://localhost" + rawPath
		want := "http://localhost" + wantPath
		if got := parseURLAddress(raw).normalform(); got != want {
			t.Errorf("normalform(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestYaCyMultiProtocolURLNormalformFixtures(t *testing.T) {
	cases := map[string]string{
		"http://www.heise.de/newsticker/thema/%23saukontrovers":          "http://www.heise.de/newsticker/thema/%23saukontrovers",
		"http://www.heise.de/newsticker/thema/#saukontrovers":            "http://www.heise.de/newsticker/thema/",
		"http://www.liferay.com/community/wiki/-/wiki/Main/Wiki+Portlet": "http://www.liferay.com/community/wiki/-/wiki/Main/Wiki+Portlet",
		"http://de.wikipedia.org/wiki/Philippe_Ariès":                    "http://de.wikipedia.org/wiki/Philippe_Ari%C3%A8s",
		"https://zh.wikipedia.org/wiki/Wikipedia:方針與指引":                  "https://zh.wikipedia.org/wiki/Wikipedia:%E6%96%B9%E9%87%9D%E8%88%87%E6%8C%87%E5%BC%95",
		"http://教育部.中国/jyb_xwfb/":                                        "http://xn--wcvs22dzol.xn--fiqs8s/jyb_xwfb/",
	}
	for raw, want := range cases {
		if got := parseURLAddress(raw).normalform(); got != want {
			t.Errorf("normalform(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestURLNormalformEdgeFixtures(t *testing.T) {
	credentialURL := "http://user:pa" + "ss@example.com/path"
	cases := map[string]string{
		"not a url":                        "http://not a url",
		credentialURL:                      credentialURL,
		"ftp://anonymous@example.com/path": "ftp://example.com/path",
		"smb://example.com/share":          "smb://example.com/share",
		"file://server/share/file.txt":     "file:///server/share/file.txt",
		"file://":                          "file:///",
		"file:///tmp/test.html":            "file:///tmp/test.html",
	}
	for raw, want := range cases {
		if got := parseURLAddress(raw).normalform(); got != want {
			t.Fatalf("normalform(%q) = %q, want %q", raw, got, want)
		}
	}
}

func hostHashForURL(t *testing.T, raw string) string {
	t.Helper()
	hash := hashForURL(t, raw)
	hostHash, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash(%q): %v", raw, err)
	}
	return hostHash
}

func hashForURL(t *testing.T, raw string) URLHash {
	t.Helper()
	hash, err := HashURL(raw)
	if err != nil {
		t.Fatalf("HashURL(%q): %v", raw, err)
	}
	return hash
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
	if h.Hash() != Hash(h.String()) {
		t.Errorf("Hash() = %q, want %q", h.Hash(), h.String())
	}
}

func TestURLHashHostHashRejectsInvalidReceiver(t *testing.T) {
	if _, err := URLHash("short").HostHash(); err == nil {
		t.Fatal("invalid URLHash receiver should fail")
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

func TestHashURLHostRejectsInvalidHosts(t *testing.T) {
	for _, host := range []string{"", "[bad", ":"} {
		if _, err := HashURLHost(host); err == nil {
			t.Fatalf("HashURLHost(%q) should fail", host)
		}
	}
}

func TestHashURLIPv6Host(t *testing.T) {
	if _, err := HashURL("http://[2001:db8::1]/"); err != nil {
		t.Fatalf("HashURL IPv6: %v", err)
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
		"":            tldLocalID,
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

func TestIsLocalHostEmpty(t *testing.T) {
	if !isLocalHost("") {
		t.Fatal("empty host should be local")
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

func TestFileRootpathNormalizesBackslashes(t *testing.T) {
	got := (urlAddress{protocol: "file", path: `\root\child`}).rootpath()
	if got != "root" {
		t.Fatalf("file rootpath = %q", got)
	}
}

func TestNormalizeFilePathEdges(t *testing.T) {
	cases := []struct {
		host string
		path string
		want string
	}{
		{"server", "share/file.txt", "/server/share/file.txt"},
		{"", "C:/tmp/test.html", "/C:/tmp/test.html"},
	}
	for _, c := range cases {
		if got := normalizeFilePath(c.host, c.path); got != c.want {
			t.Fatalf("normalizeFilePath(%q, %q) = %q, want %q", c.host, c.path, got, c.want)
		}
	}
}
