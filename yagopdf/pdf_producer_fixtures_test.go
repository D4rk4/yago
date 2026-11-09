package yagopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	producerFixtureHarbor    = "Harbor lanterns guide the midnight ferry across the quiet bay."
	producerFixtureLigatures = "The official affidavit flourished after five difficult reflections."
	producerFixtureRussian   = "Съешь же ещё этих мягких французских булок, да выпей чаю."
	producerFixtureJapanese  = "東京の図書館で静かに本を読む。"
	producerFixtureInvoice   = "Invoice 2026-11-08 totals 1,234.56 EUR (net)."
)

type producerFixture struct {
	name      string
	title     string
	sentences []string
}

var (
	producerFixtureEverySentence = []string{
		producerFixtureHarbor, encryptedFixtureKettle, producerFixtureLigatures,
		producerFixtureRussian, producerFixtureJapanese, encryptedFixtureOwls,
		producerFixtureInvoice,
	}
	producerFixtureLatinSentences = []string{
		producerFixtureHarbor, encryptedFixtureKettle, producerFixtureLigatures,
		encryptedFixtureOwls, producerFixtureInvoice,
	}
	producerFixtureShortSentences = []string{
		producerFixtureHarbor, encryptedFixtureKettle, encryptedFixtureOwls,
	}
)

var producerFixtures = []producerFixture{
	{"chromium-skia.pdf", encryptedFixtureTitle, producerFixtureEverySentence},
	{"poppler-cairo-resaved.pdf", "", producerFixtureEverySentence},
	{"pdftex-object-streams.pdf", encryptedFixtureTitle, producerFixtureLatinSentences},
	{"qpdf-qdf.pdf", encryptedFixtureTitle, producerFixtureLatinSentences},
	{"luatex-opentype.pdf", encryptedFixtureTitle, []string{
		producerFixtureHarbor, producerFixtureLigatures, producerFixtureRussian,
		encryptedFixtureOwls,
	}},
	{"libreoffice-pdfa2b-tagged.pdf", encryptedFixtureTitle, producerFixtureEverySentence},
	{
		"qpdf-libreoffice-object-streams-linearized.pdf", encryptedFixtureTitle,
		producerFixtureEverySentence,
	},
	{"ghostscript-pdfwrite.pdf", encryptedFixtureTitle, producerFixtureLatinSentences},
	{"groff-gropdf.pdf", "", producerFixtureShortSentences},
	{"reportlab-canvas.pdf", encryptedFixtureTitle, []string{
		producerFixtureHarbor, encryptedFixtureKettle, producerFixtureRussian,
		encryptedFixtureOwls, producerFixtureInvoice,
	}},
	{"tesseract-ocr-layer.pdf", "", producerFixtureShortSentences},
}

func TestReadPDFExtractsProducerFixtures(t *testing.T) {
	for _, fixture := range producerFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "producers", fixture.name))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			document, readable := ReadPDF(body)
			if !readable {
				t.Fatal("ReadPDF refused the fixture")
			}
			if document.Title != fixture.title {
				t.Errorf("title = %q, want %q", document.Title, fixture.title)
			}
			missing := producerFixtureMissingSentences(document.Text, fixture.sentences)
			if len(missing) > 0 {
				t.Errorf("text %q misses %q", document.Text, missing)
			}
		})
	}
}

func producerFixtureMissingSentences(text string, sentences []string) []string {
	var missing []string
	for _, sentence := range sentences {
		if !strings.Contains(text, sentence) {
			missing = append(missing, sentence)
		}
	}

	return missing
}
