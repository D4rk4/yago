package yagopdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"strings"
)

const (
	pdfObjectNumberKeyBytes     = 3
	pdfObjectGenerationKeyBytes = 2
	pdfObjectKeyExtensionBytes  = 5
	pdfAESObjectKeySalt         = "sAlT"
)

func (s pdfStandardSecurity) decrypted(
	method pdfCryptMethod,
	reference string,
	data []byte,
) []byte {
	switch method {
	case pdfCryptRC4:
		return pdfRC4Transformed(s.objectKey(reference, ""), data)
	case pdfCryptAES128:
		return pdfAESDecrypted(s.objectKey(reference, pdfAESObjectKeySalt), data)
	case pdfCryptAES256:
		return pdfAESDecrypted(s.fileKey, data)
	default:
		return data
	}
}

func (s pdfStandardSecurity) objectKey(reference string, salt string) []byte {
	number, generation, _ := strings.Cut(reference, " ")
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-md5
	digest := md5.New()
	_, _ = digest.Write(s.fileKey)
	_, _ = digest.Write(pdfLowOrderBytes(number, pdfObjectNumberKeyBytes))
	_, _ = digest.Write(pdfLowOrderBytes(generation, pdfObjectGenerationKeyBytes))
	_, _ = digest.Write([]byte(salt))

	return digest.Sum(nil)[:min(len(s.fileKey)+pdfObjectKeyExtensionBytes, md5.Size)]
}

func pdfLowOrderBytes(decimal string, width int) []byte {
	var value uint32
	for _, digit := range []byte(decimal) {
		value = value*10 + uint32(digit-'0')
	}

	return binary.LittleEndian.AppendUint32(nil, value)[:width]
}

func pdfCipherStreamPayload(stream pdfEncodedStream, method pdfCryptMethod) []byte {
	encoded := stream.encoded
	if method == pdfCryptAES128 || method == pdfCryptAES256 {
		whole := len(encoded) - len(encoded)%aes.BlockSize
		if len(bytes.Trim(encoded[whole:], "\r\n")) > 0 {
			return nil
		}

		return encoded[:whole]
	}
	lengthEntry := pdfDictionaryEntryValue(stream.dictionary, "Length")
	length, _, direct := pdfObjectStreamInteger(lengthEntry, 0)
	if direct && pdfLeadingReference(lengthEntry) == "" && length <= len(encoded) {
		return encoded[:length]
	}
	for _, endOfLine := range []string{"\r\n", "\n", "\r"} {
		if data, found := bytes.CutSuffix(encoded, []byte(endOfLine)); found {
			return data
		}
	}

	return encoded
}

func pdfRC4Transformed(key, data []byte) []byte {
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-rc4
	stream, _ := rc4.NewCipher(key)
	transformed := make([]byte, len(data))
	stream.XORKeyStream(transformed, data)

	return transformed
}

func pdfAESDecrypted(key, data []byte) []byte {
	if len(data) < aes.BlockSize {
		return nil
	}
	plaintext := pdfAESCipherBlocks(
		key,
		data[:aes.BlockSize],
		data[aes.BlockSize:],
		cipher.NewCBCDecrypter,
	)
	if len(plaintext) == 0 {
		return nil
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding == 0 || padding > aes.BlockSize {
		return nil
	}

	return plaintext[:len(plaintext)-padding]
}

func pdfAESCipherBlocks(
	key, initializationVector, data []byte,
	mode func(cipher.Block, []byte) cipher.BlockMode,
) []byte {
	block, err := aes.NewCipher(key)
	if err != nil || len(data)%aes.BlockSize != 0 {
		return nil
	}
	transformed := make([]byte, len(data))
	mode(block, initializationVector).CryptBlocks(transformed, data)

	return transformed
}
