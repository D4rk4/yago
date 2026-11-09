package yagopdf

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func rc4SecurityForTest() pdfStandardSecurity {
	return pdfStandardSecurity{
		fileKey:         cipherTestFileKey,
		streamMethod:    pdfCryptRC4,
		stringMethod:    pdfCryptRC4,
		encryptMetadata: true,
	}
}

func rc4HexStringForTest(security pdfStandardSecurity, reference, plaintext string) string {
	return "<" + hex.EncodeToString(
		pdfRC4Transformed(security.objectKey(reference, ""), []byte(plaintext)),
	) + ">"
}

func TestPDFDecryptedBodyReturnsUnencryptedBodyItself(t *testing.T) {
	for _, body := range [][]byte{
		[]byte("%PDF-1.4\n1 0 obj\n<< /Title (Plain) >>\nendobj\ntrailer << /Root 1 0 R >>\n"),
		[]byte("%PDF-1.4\n1 0 obj\n<< /EncryptMetadata false >>\nendobj\ntrailer << >>\n"),
	} {
		decrypted, readable := pdfDecryptedBody(body)
		if !readable || len(decrypted) != len(body) || &decrypted[0] != &body[0] {
			t.Fatalf(
				"pdfDecryptedBody(%q) = %q, %v; want the same slice",
				body,
				decrypted,
				readable,
			)
		}
	}
}

func TestPDFDecryptedBodyDecryptsStringsAndKeepsFraming(t *testing.T) {
	body := readEncryptedFixture(t, aes128FixtureName)
	decrypted, readable := pdfDecryptedBody(body)
	if !readable {
		t.Fatal("pdfDecryptedBody refused the fixture")
	}
	title := "/Title <" + hex.EncodeToString([]byte(encryptedFixtureTitle)) + ">"
	header, _, _ := bytes.Cut(body, []byte("1 0 obj"))
	_, encryption, _ := bytes.Cut(body, []byte("8 0 obj"))
	encryption, _, _ = bytes.Cut(encryption, []byte("endobj"))
	_, trailer, _ := bytes.Cut(body, []byte("trailer"))
	for _, kept := range [][]byte{
		[]byte(title),
		header,
		encryption,
		trailer,
		[]byte(">>\nstream\n"),
		[]byte("\nendstream\nendobj\n"),
	} {
		if !bytes.Contains(decrypted, kept) {
			t.Fatalf("decrypted body misses %q", kept)
		}
	}
	if bytes.Contains(body, []byte(title)) {
		t.Fatal("the encrypted fixture already carried the plaintext title")
	}
}

func TestPDFStandardSecurityDecryptsEveryStringForm(t *testing.T) {
	security := rc4SecurityForTest()
	value := "<< /Hex " + rc4HexStringForTest(security, "3 0", "north") +
		" /Literal (plain) /Nested << /Deep <> >> % (comment)\n /Open (tail"
	expected := "<< /Hex <" + hex.EncodeToString([]byte("north")) +
		"> /Literal " + rc4HexStringForTest(security, "3 0", "plain") +
		" /Nested << /Deep <> >> % (comment)\n /Open " + rc4HexStringForTest(security, "3 0", "tail")
	if decrypted := security.decryptedStrings("3 0", []byte(value)); string(decrypted) != expected {
		t.Fatalf("decrypted strings = %q, want %q", decrypted, expected)
	}
	for _, malformed := range []string{"<< /Bad <zz> >>", "<< /Open <4142"} {
		decrypted := security.decryptedStrings("3 0", []byte(malformed))
		if !bytes.Contains(decrypted, []byte("<>")) || bytes.Contains(decrypted, []byte("4142")) {
			t.Fatalf("malformed %q decrypted to %q", malformed, decrypted)
		}
	}
	security.stringMethod = pdfCryptIdentity
	clear := []byte("<< /Title (Clear) >>")
	if decrypted := security.decryptedStrings("3 0", clear); &decrypted[0] != &clear[0] {
		t.Fatalf("identity strings = %q; want the same slice", decrypted)
	}
}

func TestPDFStandardSecurityDecryptsStreamsUnlessTheyStayInClear(t *testing.T) {
	security := pdfStandardSecurity{
		fileKey:         cipherTestFileKey,
		streamMethod:    pdfCryptAES128,
		stringMethod:    pdfCryptRC4,
		encryptMetadata: false,
	}
	plaintext := "BT (Granite owls) Tj ET"
	encrypted := aesEncryptedForTest(
		t,
		security.objectKey("5 0", pdfAESObjectKeySalt),
		[]byte(plaintext),
	)
	stream := func(dictionary string, data []byte) pdfIndirectObject {
		return pdfIndirectObject{
			reference: "5 0",
			value: []byte(
				"\n" + dictionary + "\nstream\r\n" + string(data) + "\r\nendstream\n",
			),
		}
	}
	for _, test := range []struct {
		name     string
		object   pdfIndirectObject
		expected string
	}{
		{
			"content stream",
			stream("<< /Length 48 >>", encrypted),
			"\n<< /Length 23 >>\nstream\r\n" + plaintext + "\nendstream\n",
		},
		{
			"stream dictionary string",
			stream("<< /Name (n) >>", encrypted),
			"\n<< /Name " + rc4HexStringForTest(security, "5 0", "n") + " >>\nstream\r\n" +
				plaintext + "\nendstream\n",
		},
		{
			"truncated ciphertext",
			stream("<< >>", encrypted[:len(encrypted)-5]),
			"\n<< >>\nstream\r\n\nendstream\n",
		},
		{
			"cross-reference stream",
			stream("<< /Type /XRef /ID [(i)] >>", encrypted),
			string(stream("<< /Type /XRef /ID [(i)] >>", encrypted).value),
		},
		{
			"clear metadata",
			stream("<< /Type /Metadata >>", encrypted),
			string(stream("<< /Type /Metadata >>", encrypted).value),
		},
	} {
		if decrypted := security.decryptedObject(test.object); string(decrypted) != test.expected {
			t.Fatalf("%s decrypted to %q, want %q", test.name, decrypted, test.expected)
		}
	}
	security.encryptMetadata = true
	metadata := security.decryptedObject(stream("<< /Type /Metadata >>", encrypted))
	if !bytes.Contains(metadata, []byte(plaintext)) {
		t.Fatalf("encrypted metadata stayed encrypted: %q", metadata)
	}
}

func TestPDFDecryptedObjectsRewriteOnlyTheLatestObjectDefinitions(t *testing.T) {
	security := rc4SecurityForTest()
	stale := "1 0 obj\n<< /Title " + rc4HexStringForTest(security, "1 0", "stale") + " >>\nendobj\n"
	encryption := "2 0 obj\n<< /Filter /Standard /O (owner) >>\nendobj\n"
	trailer := "trailer << /Encrypt 2 0 R /ID [<00>] >>\n%%EOF\n"
	body := []byte("%PDF-1.7\n" + stale + encryption +
		"1 0 obj\n<< /Title " + rc4HexStringForTest(security, "1 0", "fresh") + " >>\nendobj\n" +
		trailer)
	expected := "%PDF-1.7\n" + stale + encryption +
		"1 0 obj\n<< /Title <" + hex.EncodeToString([]byte("fresh")) + "> >>\nendobj\n" + trailer
	decrypted := security.decryptedObjects(body, pdfScanIndirectObjects(body), "2 0")
	if string(decrypted) != expected {
		t.Fatalf("decrypted objects = %q, want %q", decrypted, expected)
	}
}

func TestReadPDFDecryptsDirectEncryptionDictionaries(t *testing.T) {
	body := readEncryptedFixture(t, rc4FixtureName)
	body = bytes.Replace(
		body,
		[]byte("/Encrypt 8 0 R"),
		[]byte("/Encrypt "+rc4FixtureDictionary),
		1,
	)
	body = bytes.Replace(body, []byte("8 0 obj\n"+rc4FixtureDictionary+"\nendobj\n"), nil, 1)
	if bytes.Contains(body, []byte("8 0 obj")) {
		t.Fatal("fixture still carries the indirect encryption dictionary")
	}
	document, readable := ReadPDF(body)
	if !readable || document.Title != encryptedFixtureTitle {
		t.Fatalf("ReadPDF = %+v, %v", document, readable)
	}
}
