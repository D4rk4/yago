package yagopdf

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPDFTextCollectorAdmitsReadableRunes(t *testing.T) {
	readable := "Plain\tline\nСъешь 東京 کتاب\u200Cها क्\u200Dष €"
	collector := newPDFTextCollector(pdfMaxTextBytes)
	collector.writeString(readable)
	if collector.String() != readable {
		t.Fatalf("readable text = %q, want %q", collector.String(), readable)
	}
}

func TestPDFTextCollectorExpandsLatinLigatures(t *testing.T) {
	collector := newPDFTextCollector(pdfMaxTextBytes)
	collector.writeString("oﬃcial ﬀ ﬁ ﬂ ﬄ ﬅ ﬆ ﬇ ﬓ")
	if want := "official ff fi fl ffl st st ﬇ ﬓ"; collector.String() != want {
		t.Fatalf("ligature text = %q, want %q", collector.String(), want)
	}
	bounded := newPDFTextCollector(2)
	bounded.writeRune(0xFB03)
	if bounded.String() != "ff" || !bounded.complete() {
		t.Fatalf("bounded ligature = %q", bounded.String())
	}
}

func TestPDFTextCollectorRefusesControlAndFormattingRunes(t *testing.T) {
	collector := newPDFTextCollector(pdfMaxTextBytes)
	collector.writeString("a\x00b\x07c\x7fd\u009be\u202Ef\u2066g\u061Ch\uFFFEi\uFDD0j\U0010FFFFk")
	if collector.String() != "abcdefghijk" {
		t.Fatalf("control text = %q", collector.String())
	}
	spacing := newPDFTextCollector(pdfMaxTextBytes)
	spacing.writeString("a\rb\vc\fd\u0085e")
	if spacing.String() != "a b c d e" {
		t.Fatalf("control spacing = %q", spacing.String())
	}
}

func TestPDFTextCollectorCutsStringsAtRuneBoundaries(t *testing.T) {
	collector := newPDFTextCollector(4)
	collector.writeString("Aé€")
	if collector.String() != "Aé" || !collector.complete() {
		t.Fatalf("bounded string = %q", collector.String())
	}
	invalid := newPDFTextCollector(pdfMaxTextBytes)
	invalid.writeString("A\xffB")
	if invalid.String() != "A�B" {
		t.Fatalf("invalid UTF-8 string = %q", invalid.String())
	}
}

func TestPDFTitleIsBoundedAndClean(t *testing.T) {
	body := []byte("%PDF-1.7\n1 0 obj << /Title (" + strings.Repeat("Aé", pdfMaxTitleBytes) +
		") >> endobj\ntrailer << /Info 1 0 R >>\n")
	title := pdfInfoTitle(body)
	if len(title) > pdfMaxTitleBytes || len(title) < pdfMaxTitleBytes-1 ||
		!utf8.ValidString(title) {
		t.Fatalf("title = %d bytes, valid UTF-8 = %t", len(title), utf8.ValidString(title))
	}
	hostile := []byte("%PDF-1.7\n1 0 obj << /Title <FEFF00480069000900000007000A202E00740021> >>" +
		" endobj\ntrailer << /Info 1 0 R >>\n")
	if title := pdfInfoTitle(hostile); title != "Hi t!" {
		t.Fatalf("hostile title = %q", title)
	}
}
