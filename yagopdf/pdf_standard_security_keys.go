package yagopdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
)

const (
	pdfRC4EraEntryBytes             = 32
	pdfRC4EraKeyIterations          = 50
	pdfRC4EraUserEntryRounds        = 20
	pdfAES256EraEntryBytes          = 48
	pdfAES256EraKeyEntryBytes       = 32
	pdfHardenedHashRepetitions      = 64
	pdfHardenedHashMinimumRounds    = 64
	pdfHardenedHashRoundAllowance   = 32
	pdfHardenedHashBlockSelectBytes = 16
)

var pdfPasswordPadding = []byte{
	0x28, 0xbf, 0x4e, 0x5e, 0x4e, 0x75, 0x8a, 0x41, 0x64, 0x00, 0x4e, 0x56, 0xff, 0xfa, 0x01, 0x08,
	0x2e, 0x2e, 0x00, 0xb6, 0xd0, 0x68, 0x3e, 0x80, 0x2f, 0x0c, 0xa9, 0xfe, 0x64, 0x53, 0x69, 0x7a,
}

func pdfEmptyUserPasswordFileKey(entries pdfStandardSecurityEntries) ([]byte, bool) {
	switch {
	case entries.revision >= 2 && entries.revision <= 4 && entries.version != 5:
		return pdfRC4EraFileKey(entries)
	case (entries.revision == 5 || entries.revision == 6) && entries.version == 5:
		return pdfAES256EraFileKey(entries)
	default:
		return nil, false
	}
}

func pdfRC4EraFileKey(entries pdfStandardSecurityEntries) ([]byte, bool) {
	if len(entries.owner) < pdfRC4EraEntryBytes || len(entries.user) < pdfRC4EraEntryBytes {
		return nil, false
	}
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
	digest := md5.New()
	_, _ = digest.Write(pdfPasswordPadding)
	_, _ = digest.Write(entries.owner[:pdfRC4EraEntryBytes])
	_, _ = digest.Write(entries.permissions)
	_, _ = digest.Write(entries.firstIdentifier)
	if entries.revision >= 4 && !entries.encryptMetadata {
		_, _ = digest.Write([]byte{0xff, 0xff, 0xff, 0xff})
	}
	fileKey := digest.Sum(nil)[:entries.keyBytes]
	for round := 0; entries.revision >= 3 && round < pdfRC4EraKeyIterations; round++ {
		// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
		sum := md5.Sum(fileKey)
		fileKey = sum[:entries.keyBytes]
	}

	return fileKey, pdfRC4EraUserEntryMatches(fileKey, entries)
}

func pdfRC4EraUserEntryMatches(fileKey []byte, entries pdfStandardSecurityEntries) bool {
	if entries.revision == 2 {
		return bytes.Equal(
			pdfRC4Transformed(fileKey, pdfPasswordPadding),
			entries.user[:pdfRC4EraEntryBytes],
		)
	}
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
	digest := md5.New()
	_, _ = digest.Write(pdfPasswordPadding)
	_, _ = digest.Write(entries.firstIdentifier)
	value := digest.Sum(nil)
	roundKey := make([]byte, len(fileKey))
	for round := byte(0); round < pdfRC4EraUserEntryRounds; round++ {
		for index, keyByte := range fileKey {
			roundKey[index] = keyByte ^ round
		}
		value = pdfRC4Transformed(roundKey, value)
	}

	return bytes.Equal(value, entries.user[:md5.Size])
}

func pdfAES256EraFileKey(entries pdfStandardSecurityEntries) ([]byte, bool) {
	if len(entries.owner) < pdfAES256EraEntryBytes || len(entries.user) < pdfAES256EraEntryBytes ||
		len(entries.ownerKey) != pdfAES256EraKeyEntryBytes ||
		len(entries.userKey) != pdfAES256EraKeyEntryBytes {
		return nil, false
	}
	validation := pdfEmptyPasswordHash(entries.revision, entries.user[32:40])
	if !bytes.Equal(validation, entries.user[:32]) {
		return nil, false
	}

	return pdfAESCipherBlocks(
		pdfEmptyPasswordHash(entries.revision, entries.user[40:48]),
		make([]byte, aes.BlockSize),
		entries.userKey,
		cipher.NewCBCDecrypter,
	), true
}

func pdfEmptyPasswordHash(revision int, salt []byte) []byte {
	initial := sha256.Sum256(salt)
	if revision == 5 {
		return initial[:]
	}

	return pdfHardenedHash(initial[:])
}

func pdfHardenedHash(initial []byte) []byte {
	digest := initial
	for round := 1; ; round++ {
		encrypted := pdfAESCipherBlocks(
			digest[:aes.BlockSize],
			digest[aes.BlockSize:2*aes.BlockSize],
			bytes.Repeat(digest, pdfHardenedHashRepetitions),
			cipher.NewCBCEncrypter,
		)
		digest = pdfHardenedHashRoundDigest(encrypted)
		if pdfHardenedHashFinished(round, encrypted[len(encrypted)-1]) {
			return digest[:sha256.Size]
		}
	}
}

func pdfHardenedHashRoundDigest(encrypted []byte) []byte {
	remainder := 0
	for _, value := range encrypted[:pdfHardenedHashBlockSelectBytes] {
		remainder += int(value)
	}
	switch remainder % 3 {
	case 0:
		sum := sha256.Sum256(encrypted)

		return sum[:]
	case 1:
		sum := sha512.Sum384(encrypted)

		return sum[:]
	default:
		sum := sha512.Sum512(encrypted)

		return sum[:]
	}
}

func pdfHardenedHashFinished(round int, lastByte byte) bool {
	return round >= pdfHardenedHashMinimumRounds &&
		int(lastByte) <= round-pdfHardenedHashRoundAllowance
}
