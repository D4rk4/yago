package yagopdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

var (
	cipherTestFileKey              = []byte("sixteen byte key")
	cipherTestInitializationVector = []byte("initial vector16")
)

func cbcEncryptedForTest(t *testing.T, key, initializationVector, blocks []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes key: %v", err)
	}
	encrypted := make([]byte, len(blocks))
	cipher.NewCBCEncrypter(block, initializationVector).CryptBlocks(encrypted, blocks)

	return append(bytes.Clone(initializationVector), encrypted...)
}

func aesEncryptedForTest(t *testing.T, key, plaintext []byte) []byte {
	t.Helper()
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(bytes.Clone(plaintext), bytes.Repeat([]byte{byte(padding)}, padding)...)

	return cbcEncryptedForTest(t, key, cipherTestInitializationVector, padded)
}

func TestPDFAESDecryptedRefusesMalformedCiphertext(t *testing.T) {
	plaintext := []byte("Granite owls observe the winter observatory schedule.")
	encrypted := aesEncryptedForTest(t, cipherTestFileKey, plaintext)
	if decrypted := pdfAESDecrypted(
		cipherTestFileKey,
		encrypted,
	); !bytes.Equal(
		decrypted,
		plaintext,
	) {
		t.Fatalf("round trip = %q", decrypted)
	}
	zeroPadding := cbcEncryptedForTest(t, cipherTestFileKey, cipherTestInitializationVector,
		make([]byte, aes.BlockSize))
	oversizedPadding := cbcEncryptedForTest(t, cipherTestFileKey, cipherTestInitializationVector,
		bytes.Repeat([]byte{aes.BlockSize + 1}, aes.BlockSize))
	for _, test := range []struct {
		name string
		key  []byte
		data []byte
	}{
		{"shorter than one block", cipherTestFileKey, encrypted[:aes.BlockSize-1]},
		{"vector without blocks", cipherTestFileKey, encrypted[:aes.BlockSize]},
		{"truncated block", cipherTestFileKey, encrypted[:len(encrypted)-5]},
		{"zero padding", cipherTestFileKey, zeroPadding},
		{"padding above one block", cipherTestFileKey, oversizedPadding},
		{"key of invalid size", cipherTestFileKey[:10], encrypted},
	} {
		if decrypted := pdfAESDecrypted(test.key, test.data); decrypted != nil {
			t.Fatalf("%s decrypted to %q", test.name, decrypted)
		}
	}
}

func TestPDFObjectKeyFollowsTheObjectNumberAndGeneration(t *testing.T) {
	security := pdfStandardSecurity{fileKey: cipherTestFileKey[:5]}
	if key := hex.EncodeToString(security.objectKey("70000 3", "")); key != "38368482a7759c97b8f6" {
		t.Fatalf("object key = %s", key)
	}
	security.fileKey = cipherTestFileKey
	if key := hex.EncodeToString(security.objectKey("16777217 0", pdfAESObjectKeySalt)); key !=
		"00ee5a3d7cf93fa101aa14886c2fafbf" {
		t.Fatalf("salted object key = %s", key)
	}
}

func TestPDFStandardSecurityDecryptedFollowsTheCryptMethod(t *testing.T) {
	security := pdfStandardSecurity{fileKey: cipherTestFileKey}
	plaintext := []byte("Seventeen copper kettles whistle in the orchard kitchen.")
	rc4Encrypted := pdfRC4Transformed(security.objectKey("4 0", ""), plaintext)
	aes128Encrypted := aesEncryptedForTest(
		t,
		security.objectKey("4 0", pdfAESObjectKeySalt),
		plaintext,
	)
	aes256Key := bytes.Repeat(cipherTestFileKey, 2)
	aes256Encrypted := aesEncryptedForTest(t, aes256Key, plaintext)
	for _, test := range []struct {
		method    pdfCryptMethod
		fileKey   []byte
		encrypted []byte
	}{
		{pdfCryptRC4, cipherTestFileKey, rc4Encrypted},
		{pdfCryptAES128, cipherTestFileKey, aes128Encrypted},
		{pdfCryptAES256, aes256Key, aes256Encrypted},
		{pdfCryptIdentity, cipherTestFileKey, plaintext},
	} {
		security.fileKey = test.fileKey
		if decrypted := security.decrypted(test.method, "4 0", test.encrypted); !bytes.Equal(
			decrypted,
			plaintext,
		) {
			t.Fatalf("method %d decrypted to %q", test.method, decrypted)
		}
	}
	if decrypted := security.decrypted(pdfCryptRC4, "5 0", rc4Encrypted); bytes.Equal(
		decrypted,
		plaintext,
	) {
		t.Fatal("another object's key decrypted the ciphertext")
	}
}

func TestPDFCipherStreamPayloadTrimsOnlyTheStreamEnd(t *testing.T) {
	aligned := bytes.Repeat([]byte{0xa5}, 2*aes.BlockSize)
	for _, test := range []struct {
		name       string
		dictionary string
		encoded    []byte
		method     pdfCryptMethod
		payload    []byte
	}{
		{"block cipher before line end", "<< >>", append(bytes.Clone(aligned), '\r', '\n'), pdfCryptAES128, aligned},
		{"block cipher without line end", "<< >>", aligned, pdfCryptAES256, aligned},
		{"block cipher with stray bytes", "<< >>", append(bytes.Clone(aligned), 'x'), pdfCryptAES128, nil},
		{"stream cipher within direct length", "<< /Length 3 >>", []byte("ab\n\n"), pdfCryptRC4, []byte("ab\n")},
		{"stream cipher with indirect length", "<< /Length 3 0 R >>", []byte("ab\r\n"), pdfCryptRC4, []byte("ab")},
		{"stream cipher with excess length", "<< /Length 9 >>", []byte("ab\n"), pdfCryptRC4, []byte("ab")},
		{"stream cipher with carriage return", "<< >>", []byte("ab\r"), pdfCryptRC4, []byte("ab")},
		{"stream cipher without line end", "<< >>", []byte("ab"), pdfCryptRC4, []byte("ab")},
	} {
		payload := pdfCipherStreamPayload(
			pdfEncodedStream{dictionary: []byte(test.dictionary), encoded: test.encoded},
			test.method,
		)
		if !bytes.Equal(payload, test.payload) || (payload == nil) != (test.payload == nil) {
			t.Fatalf("%s payload = %q, want %q", test.name, payload, test.payload)
		}
	}
}
