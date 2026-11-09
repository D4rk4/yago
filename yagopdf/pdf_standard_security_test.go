package yagopdf

import (
	"bytes"
	"testing"
)

const (
	rc4FixtureName        = "qpdf-rc4-128-r3.pdf"
	aes128FixtureName     = "qpdf-aes-128-r4.pdf"
	aes256FixtureName     = "qpdf-aes-256-r6.pdf"
	rc4FixtureOwnerEntry  = "/O <913b073f5b65954c8b6da22169971a177b7e6d94a106ff75c0c16296bc15412e>"
	rc4FixtureUserEntry   = "/U <b2b1b6594888b339eb4c38af385da2650021446990b9e4114071a4d9104984c1>"
	rc4FixtureIdentifier  = "/ID [<31415926535897932384626433832795><31415926535897932384626433832795>]"
	aes256FixtureUserKey  = "/UE <0aa678d023a832a6b508fe8b2f0bec9732eebe4732c455ec2a193ed0d9d8da7a>"
	aes256FixtureOwnerKey = "/OE <357d266eeb1e7dc855c7d25a7c6f5b0faa6af872fa62cfa48452c6668c779e2c>"
	aes256FixtureUserHead = "/U <76336b4f796192c6fe92a74d3fee54eab4c1c99e8f5c87deb907f0cf63e2caa9"
	rc4FixtureDictionary  = "<< /Filter /Standard /Length 128 " + rc4FixtureOwnerEntry +
		" /P -4 /R 3 " + rc4FixtureUserEntry + " /V 2 >>"
)

func encryptedFixtureWith(t *testing.T, name string, replacements ...string) []byte {
	t.Helper()
	body := readEncryptedFixture(t, name)
	for index := 0; index+1 < len(replacements); index += 2 {
		if !bytes.Contains(body, []byte(replacements[index])) {
			t.Fatalf("fixture %s lacks %q", name, replacements[index])
		}
		body = bytes.Replace(body, []byte(replacements[index]), []byte(replacements[index+1]), 1)
	}

	return body
}

func TestPDFDecryptedBodyRefusesHostileEncryptionDictionaries(t *testing.T) {
	for _, test := range []struct {
		name         string
		fixture      string
		replacements []string
	}{
		{"public key handler", rc4FixtureName, []string{"/Standard", "/Adobe.PubSec"}},
		{"missing handler", rc4FixtureName, []string{"/Filter /Standard ", ""}},
		{"unknown revision", rc4FixtureName, []string{"/R 3", "/R 7"}},
		{"revision below two", rc4FixtureName, []string{"/R 3", "/R 1"}},
		{"unpublished version", rc4FixtureName, []string{"/V 2", "/V 3"}},
		{"version zero", rc4FixtureName, []string{"/V 2", "/V 0"}},
		{"oversized key length", rc4FixtureName, []string{"/Length 128", "/Length 4096"}},
		{"zero key length", rc4FixtureName, []string{"/Length 128", "/Length 0"}},
		{"key length off byte boundary", rc4FixtureName, []string{"/Length 128", "/Length 39"}},
		{"indirect key length", rc4FixtureName, []string{"/Length 128", "/Length 6 0 R"}},
		{"permissions above 32 bits", rc4FixtureName, []string{"/P -4", "/P 4294967296"}},
		{"permissions below 32 bits", rc4FixtureName, []string{"/P -4", "/P -2147483649"}},
		{"permissions not a number", rc4FixtureName, []string{"/P -4", "/P /All"}},
		{"text extraction forbidden", rc4FixtureName, []string{"/P -4", "/P -20"}},
		{"missing owner entry", rc4FixtureName, []string{rc4FixtureOwnerEntry, ""}},
		{"short owner entry", rc4FixtureName, []string{rc4FixtureOwnerEntry, "/O <913b>"}},
		{"short user entry", rc4FixtureName, []string{rc4FixtureUserEntry, "/U (short)"}},
		{"wrong user password", rc4FixtureName, []string{"/U <b2b1", "/U <00b1"}},
		{"missing identifier", rc4FixtureName, []string{rc4FixtureIdentifier, ""}},
		{"malformed identifier", rc4FixtureName, []string{rc4FixtureIdentifier, "/ID 31415"}},
		{"encrypt points nowhere", rc4FixtureName, []string{"/Encrypt 8 0 R", "/Encrypt 99 0 R"}},
		{"encrypt is a number", rc4FixtureName, []string{"/Encrypt 8 0 R", "/Encrypt 8"}},
		{"self-referencing encrypt", rc4FixtureName, []string{rc4FixtureDictionary, "8 0 R"}},
		{
			"direct encrypt without entries",
			rc4FixtureName,
			[]string{"/Encrypt 8 0 R", "/Encrypt << /Filter /Standard /V 2 /R 3 /P -4 >>"},
		},
		{"unknown crypt method", aes128FixtureName, []string{"/CFM /AESV2", "/CFM /AESV9"}},
		{"missing crypt filter", aes128FixtureName, []string{"/StrF /StdCF", "/StrF /Absent"}},
		{"short user key", aes256FixtureName, []string{aes256FixtureUserKey, "/UE <0aa678>"}},
		{"short owner key", aes256FixtureName, []string{aes256FixtureOwnerKey, "/OE <357d26>"}},
		{"short hardened user entry", aes256FixtureName, []string{aes256FixtureUserHead, "/U <00"}},
		{"wrong hardened password", aes256FixtureName, []string{"/U <7633", "/U <0033"}},
		{"hardened revision as four", aes256FixtureName, []string{"/R 6", "/R 4"}},
		{"hardened revision as seven", aes256FixtureName, []string{"/R 6", "/R 7"}},
		{
			"revision six under version four",
			aes256FixtureName,
			[]string{"/V 5", "/V 4", "/Standard /Length 256", "/Standard /Length 128"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			decrypted, readable := pdfDecryptedBody(
				encryptedFixtureWith(t, test.fixture, test.replacements...),
			)
			if readable || decrypted != nil {
				t.Fatalf("pdfDecryptedBody = %d bytes, %v; want refusal", len(decrypted), readable)
			}
		})
	}
}

func TestPDFDecryptedBodyAcceptsEquivalentEncryptionDictionaryForms(t *testing.T) {
	for _, test := range []struct {
		name         string
		fixture      string
		replacements []string
	}{
		{"unsigned permissions", rc4FixtureName, []string{"/P -4", "/P 4294967292"}},
		{
			"literal identifier",
			rc4FixtureName,
			[]string{"[<31415926535897932384626433832795>", `[(1AY&SX\227\223#\204bd3\203'\225)`},
		},
		{
			"version one ignores key length",
			"qpdf-rc4-40-r2.pdf",
			[]string{"/Standard /Length 40 ", "/Standard /Length 4096 "},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := encryptedFixtureWith(t, test.fixture, test.replacements...)
			if _, readable := pdfDecryptedBody(body); !readable {
				t.Fatal("pdfDecryptedBody refused an equivalent dictionary form")
			}
			document, readable := ReadPDF(body)
			if !readable || document.Title != encryptedFixtureTitle {
				t.Fatalf("ReadPDF = %+v, %v", document, readable)
			}
		})
	}
}

func TestPDFDecryptedBodyRefusesEncryptedFixturesWithoutEmptyUserAccess(t *testing.T) {
	for _, name := range []string{
		"qpdf-aes-256-r6-user-password.pdf",
		"qpdf-rc4-128-r3-user-password.pdf",
		"qpdf-aes-128-r4-extract-forbidden.pdf",
	} {
		if decrypted, readable := pdfDecryptedBody(readEncryptedFixture(t, name)); readable ||
			decrypted != nil {
			t.Fatalf("pdfDecryptedBody opened %s", name)
		}
	}
}

func TestPDFExtractionPermissionsRequireTheExtractTextBit(t *testing.T) {
	for _, test := range []struct {
		dictionary string
		permitted  bool
	}{
		{"<< /P -4 >>", true},
		{"<< /P 16 >>", true},
		{"<< /P 4294967292 >>", true},
		{"<< /P -20 >>", false},
		{"<< /P 0 >>", false},
		{"<< /P 4294967296 >>", false},
		{"<< /P 99999999999999999999 >>", false},
		{"<< /V 2 >>", false},
	} {
		if _, permitted := pdfExtractionPermissions([]byte(test.dictionary)); permitted !=
			test.permitted {
			t.Fatalf("%s permitted = %v", test.dictionary, permitted)
		}
	}
	signed, _ := pdfExtractionPermissions([]byte("<< /P -4 >>"))
	unsigned, _ := pdfExtractionPermissions([]byte("<< /P 4294967292 >>"))
	if !bytes.Equal(signed, []byte{0xfc, 0xff, 0xff, 0xff}) || !bytes.Equal(signed, unsigned) {
		t.Fatalf("permission bytes = %x and %x", signed, unsigned)
	}
}

func TestPDFFileKeyBytesBoundsTheKeyLength(t *testing.T) {
	for _, test := range []struct {
		dictionary string
		version    int
		keyBytes   int
		valid      bool
	}{
		{"<< /Length 4096 >>", 1, 5, true},
		{"<< >>", 2, 5, true},
		{"<< /Length 40 >>", 2, 5, true},
		{"<< /Length 128 >>", 2, 16, true},
		{"<< /Length 136 >>", 2, 17, false},
		{"<< /Length 32 >>", 2, 4, false},
		{"<< /Length 44 >>", 2, 5, false},
		{"<< >>", 4, 16, true},
		{"<< /Length 256 >>", 5, 32, true},
		{"<< /Length 128 >>", 3, 0, false},
		{"<< /Length 128 >>", 6, 0, false},
	} {
		keyBytes, valid := pdfFileKeyBytes([]byte(test.dictionary), test.version)
		if valid != test.valid || valid && keyBytes != test.keyBytes {
			t.Fatalf("%s version %d = %d, %v", test.dictionary, test.version, keyBytes, valid)
		}
	}
}

func TestPDFCryptMethodsReadCryptFilters(t *testing.T) {
	filters := "/CF << /Rc << /CFM /V2 >> /Aes << /CFM /AESV2 >> /Off << /CFM /None >> >>"
	for _, test := range []struct {
		dictionary   string
		version      int
		streamMethod pdfCryptMethod
		stringMethod pdfCryptMethod
		known        bool
	}{
		{"<< >>", 2, pdfCryptRC4, pdfCryptRC4, true},
		{"<< >>", 4, pdfCryptIdentity, pdfCryptIdentity, true},
		{"<< " + filters + " /StmF /Aes /StrF /Rc >>", 4, pdfCryptAES128, pdfCryptRC4, true},
		{"<< " + filters + " /StmF /Identity /StrF /Off >>", 4, pdfCryptIdentity, pdfCryptIdentity, true},
		{"<< /CF << /V3 << /CFM /AESV3 >> >> /StmF /V3 /StrF /V3 >>", 5, pdfCryptAES256, pdfCryptAES256, true},
		{"<< " + filters + " /StmF /Aes /StrF /Missing >>", 4, pdfCryptAES128, pdfCryptIdentity, false},
		{"<< /CF << /X << /CFM /Unknown >> >> /StmF /X >>", 4, pdfCryptIdentity, pdfCryptIdentity, false},
	} {
		streamMethod, stringMethod, known := pdfCryptMethods([]byte(test.dictionary), test.version)
		if known != test.known || known &&
			(streamMethod != test.streamMethod || stringMethod != test.stringMethod) {
			t.Fatalf("%s = %d, %d, %v", test.dictionary, streamMethod, stringMethod, known)
		}
	}
}

func TestPDFHardenedHashStopsWithinItsSpecifiedRoundBound(t *testing.T) {
	const lastPossibleRound = 255 + pdfHardenedHashRoundAllowance
	for lastByte := range 256 {
		if !pdfHardenedHashFinished(lastPossibleRound, byte(lastByte)) {
			t.Fatalf("round %d continues for last byte %d", lastPossibleRound, lastByte)
		}
	}
	if pdfHardenedHashFinished(pdfHardenedHashMinimumRounds-1, 0) {
		t.Fatal("hardened hash finished before the minimum round count")
	}
	if !pdfHardenedHashFinished(pdfHardenedHashMinimumRounds, 32) {
		t.Fatal("hardened hash continued after the minimum round with a low last byte")
	}
	if pdfHardenedHashFinished(pdfHardenedHashMinimumRounds, 33) {
		t.Fatal("hardened hash finished while the last byte exceeded the round allowance")
	}
}

func TestPDFHardenedHashSelectsTheDigestByBlockRemainder(t *testing.T) {
	for _, test := range []struct {
		firstByte byte
		size      int
	}{
		{0, 32},
		{1, 48},
		{2, 64},
	} {
		encrypted := make([]byte, 64)
		encrypted[0] = test.firstByte
		if size := len(pdfHardenedHashRoundDigest(encrypted)); size != test.size {
			t.Fatalf(
				"first byte %d selects %d-byte digest, want %d",
				test.firstByte,
				size,
				test.size,
			)
		}
	}
}
