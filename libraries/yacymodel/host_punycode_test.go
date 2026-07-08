package yacymodel

import "testing"

func TestToPunycodeEncodesNonBasicLabelsPerLabel(t *testing.T) {
	cases := map[string]string{
		"example.com":    "example.com",
		"bücher.example": "xn--bcher-kva.example",
		"münchen.de":     "xn--mnchen-3ya.de",
		"例え.テスト":         "xn--r8jz45g.xn--zckzah",
		"www.bücher.com": "www.xn--bcher-kva.com",
		"127.0.0.1":      "127.0.0.1",
	}
	for in, want := range cases {
		if got := toPunycode(in); got != want {
			t.Errorf("toPunycode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURLHashUsesPunycodeHost(t *testing.T) {
	unicode, err := HashURL("http://bücher.example/")
	if err != nil {
		t.Fatalf("HashURL unicode: %v", err)
	}
	ascii, err := HashURL("http://xn--bcher-kva.example/")
	if err != nil {
		t.Fatalf("HashURL ascii: %v", err)
	}
	if unicode != ascii {
		t.Errorf("IDN url hash %q must equal punycode form %q", unicode, ascii)
	}
}

func TestDomainIDUsesPunycodeTLD(t *testing.T) {
	if got := DomainID(toPunycode("bücher.de")); got != tldEuropeRussiaID {
		t.Errorf("DomainID(punycode .de) = %d, want %d", got, tldEuropeRussiaID)
	}
}
