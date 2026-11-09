package yagopdf

import (
	"bytes"
	"testing"
)

func TestPDFDecryptedStreamLengthDescribesThePlaintext(t *testing.T) {
	security := pdfStandardSecurity{
		streamMethod: pdfCryptRC4, stringMethod: pdfCryptIdentity,
		fileKey: []byte("synthetic-key"),
	}
	for _, plaintext := range []string{
		"BT (Legitimate decrypted page words) Tj ET",
		"BT (Legitimate decrypted page words) Tj ET\nendstream\nendobj\n9 0 obj\n<< >>\nendobj\n",
	} {
		ciphertext := pdfRC4Transformed(security.objectKey("2 0", ""), []byte(plaintext))
		body := newSyntheticPDF().encodedStream(2, "", ciphertext).finished()
		object := newPDFObjectLookup(body).byReference["2 0"]
		decrypted := security.decryptedObject(object)
		stream, exists := pdfEncodedStreamOf(decrypted)
		if !exists || string(stream.encoded) != plaintext {
			t.Fatalf("decrypted stream = %t %q, want %q", exists, stream.encoded, plaintext)
		}
	}
}

func TestPDFDecryptedStreamLengthRepairKeepsOtherEntries(t *testing.T) {
	for _, dictionary := range []string{
		"<< /Length 8 /Note (unchanged) >>\n",
		"<< /Length 12 0 R /Note (unchanged) >>\n",
	} {
		repaired := pdfDecryptedStreamDictionary([]byte(dictionary), 8)
		if string(repaired) != "<< /Length 8 /Note (unchanged) >>\n" {
			t.Fatalf("repaired dictionary = %q", repaired)
		}
	}
	original := []byte("<< /Note (unchanged) >>\n")
	if repaired := pdfDecryptedStreamDictionary(original, 8); !bytes.Equal(repaired, original) {
		t.Fatalf("lengthless dictionary changed = %q", repaired)
	}
}
