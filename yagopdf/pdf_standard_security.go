package yagopdf

import (
	"bytes"
	"encoding/binary"
	"math"
	"regexp"
	"strconv"
)

const pdfExtractTextPermission = 1 << 4

type pdfCryptMethod uint8

const (
	pdfCryptIdentity pdfCryptMethod = iota
	pdfCryptRC4
	pdfCryptAES128
	pdfCryptAES256
)

var (
	pdfCryptFilterMethods = map[string]pdfCryptMethod{
		"V2":    pdfCryptRC4,
		"AESV2": pdfCryptAES128,
		"AESV3": pdfCryptAES256,
		"None":  pdfCryptIdentity,
	}
	pdfSignedIntegerPattern = regexp.MustCompile(`^[+-]?[0-9]{1,20}`)
)

type pdfStandardSecurity struct {
	fileKey         []byte
	streamMethod    pdfCryptMethod
	stringMethod    pdfCryptMethod
	encryptMetadata bool
}

type pdfStandardSecurityEntries struct {
	version         int
	revision        int
	keyBytes        int
	permissions     []byte
	owner           []byte
	user            []byte
	ownerKey        []byte
	userKey         []byte
	firstIdentifier []byte
	encryptMetadata bool
}

func pdfStandardSecurityUnlocked(dictionary, firstIdentifier []byte) (pdfStandardSecurity, bool) {
	filter, _ := pdfDictionaryName(dictionary, "Filter")
	version, _, _ := pdfObjectStreamInteger(pdfDictionaryEntryValue(dictionary, "V"), 0)
	revision, _, _ := pdfObjectStreamInteger(pdfDictionaryEntryValue(dictionary, "R"), 0)
	keyBytes, keyLengthValid := pdfFileKeyBytes(dictionary, version)
	streamMethod, stringMethod, methodsKnown := pdfCryptMethods(dictionary, version)
	permissions, extractionPermitted := pdfExtractionPermissions(dictionary)
	if filter != "Standard" || !keyLengthValid || !methodsKnown || !extractionPermitted {
		return pdfStandardSecurity{}, false
	}
	encryptMetadata := !bytes.HasPrefix(
		pdfDictionaryEntryValue(dictionary, "EncryptMetadata"),
		[]byte("false"),
	)
	fileKey, unlocked := pdfEmptyUserPasswordFileKey(pdfStandardSecurityEntries{
		version:         version,
		revision:        revision,
		keyBytes:        keyBytes,
		permissions:     permissions,
		owner:           pdfSecurityString(pdfDictionaryEntryValue(dictionary, "O")),
		user:            pdfSecurityString(pdfDictionaryEntryValue(dictionary, "U")),
		ownerKey:        pdfSecurityString(pdfDictionaryEntryValue(dictionary, "OE")),
		userKey:         pdfSecurityString(pdfDictionaryEntryValue(dictionary, "UE")),
		firstIdentifier: firstIdentifier,
		encryptMetadata: encryptMetadata,
	})

	return pdfStandardSecurity{
		fileKey:         fileKey,
		streamMethod:    streamMethod,
		stringMethod:    stringMethod,
		encryptMetadata: encryptMetadata,
	}, unlocked
}

func pdfFileKeyBytes(dictionary []byte, version int) (int, bool) {
	switch version {
	case 1:
		return 5, true
	case 2:
		return pdfVariableFileKeyBytes(dictionary, 40)
	case 4:
		return pdfVariableFileKeyBytes(dictionary, 128)
	case 5:
		return 32, true
	default:
		return 0, false
	}
}

func pdfVariableFileKeyBytes(dictionary []byte, defaultBits int) (int, bool) {
	value := pdfDictionaryEntryValue(dictionary, "Length")
	if value == nil {
		return defaultBits / 8, true
	}
	bits, _, found := pdfObjectStreamInteger(value, 0)

	return bits / 8, found && bits >= 40 && bits <= 128 && bits%8 == 0
}

func pdfCryptMethods(dictionary []byte, version int) (pdfCryptMethod, pdfCryptMethod, bool) {
	if version < 4 {
		return pdfCryptRC4, pdfCryptRC4, true
	}
	filters := pdfDirectDictionary(pdfDictionaryEntryValue(dictionary, "CF"))
	streamMethod, streamKnown := pdfCryptFilterMethod(dictionary, filters, "StmF")
	stringMethod, stringKnown := pdfCryptFilterMethod(dictionary, filters, "StrF")

	return streamMethod, stringMethod, streamKnown && stringKnown
}

func pdfCryptFilterMethod(dictionary, filters []byte, entry string) (pdfCryptMethod, bool) {
	name, named := pdfDictionaryName(dictionary, entry)
	if !named || name == "Identity" {
		return pdfCryptIdentity, true
	}
	method, _ := pdfDictionaryName(
		pdfDirectDictionary(pdfDictionaryEntryValue(filters, name)),
		"CFM",
	)
	cryptMethod, known := pdfCryptFilterMethods[method]

	return cryptMethod, known
}

func pdfExtractionPermissions(dictionary []byte) ([]byte, bool) {
	token := pdfSignedIntegerPattern.Find(pdfDictionaryEntryValue(dictionary, "P"))
	permissions, err := strconv.ParseInt(string(token), 10, 64)
	if err != nil || permissions < math.MinInt32 || permissions > math.MaxUint32 {
		return nil, false
	}

	return binary.LittleEndian.AppendUint32(nil, uint32(permissions&math.MaxUint32)),
		permissions&pdfExtractTextPermission != 0
}
