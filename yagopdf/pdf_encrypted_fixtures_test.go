package yagopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	encryptedFixtureTitle  = "Lantern Harbor Field Notes"
	encryptedFixtureOwls   = "Granite owls observe the winter observatory schedule."
	encryptedFixtureKettle = "Seventeen copper kettles whistle in the orchard kitchen."
)

func readEncryptedFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "encrypted", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return body
}

func TestReadPDFDecryptsEmptyUserPasswordFixtures(t *testing.T) {
	for _, name := range []string{
		"qpdf-rc4-40-r2.pdf",
		"qpdf-rc4-128-r3.pdf",
		"qpdf-aes-128-r4.pdf",
		"qpdf-aes-128-r4-cleartext-metadata.pdf",
		"qpdf-aes-128-r4-object-streams.pdf",
		"qpdf-aes-256-r5.pdf",
		"qpdf-aes-256-r6.pdf",
		"qpdf-aes-256-r6-object-streams.pdf",
		"ghostscript-rc4-40-r2.pdf",
		"ghostscript-rc4-128-r3.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			document, readable := ReadPDF(readEncryptedFixture(t, name))
			if !readable {
				t.Fatalf("ReadPDF refused %s", name)
			}
			if document.Title != encryptedFixtureTitle {
				t.Fatalf("title = %q", document.Title)
			}
			for _, sentence := range []string{encryptedFixtureOwls, encryptedFixtureKettle} {
				if !strings.Contains(document.Text, sentence) {
					t.Fatalf("text %q misses %q", document.Text, sentence)
				}
			}
		})
	}
}

func TestReadPDFDecryptsOwnerPasswordOnlyOfficeExport(t *testing.T) {
	document, readable := ReadPDF(readEncryptedFixture(t, "libreoffice-owner-password.pdf"))
	if !readable {
		t.Fatal("ReadPDF refused the owner-password-only export")
	}
	if document.Title != encryptedFixtureTitle {
		t.Fatalf("title = %q", document.Title)
	}
	if !strings.Contains(document.Text, encryptedFixtureOwls) {
		t.Fatalf("text %q misses %q", document.Text, encryptedFixtureOwls)
	}
}

func TestReadPDFRefusesEncryptedFixturesItCannotOpen(t *testing.T) {
	for _, name := range []string{
		"qpdf-aes-256-r6-user-password.pdf",
		"qpdf-rc4-128-r3-user-password.pdf",
		"qpdf-aes-128-r4-extract-forbidden.pdf",
	} {
		t.Run(name, func(t *testing.T) {
			if document, readable := ReadPDF(readEncryptedFixture(t, name)); readable {
				t.Fatalf("ReadPDF opened %s: %+v", name, document)
			}
		})
	}
}
