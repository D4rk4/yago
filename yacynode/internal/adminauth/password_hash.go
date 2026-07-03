package adminauth

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB   = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLength  = 16
	argonKeyLength   = 32
)

var errMalformedHash = errors.New("malformed password hash")

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := randRead(salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}

	return encodeArgon2id(
		password,
		salt,
		argon2Params{
			memory:      argonMemoryKiB,
			iterations:  argonIterations,
			parallelism: argonParallelism,
		},
		argonKeyLength,
	), nil
}

func encodeArgon2id(password string, salt []byte, params argon2Params, keyLen uint32) string {
	key := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		keyLen,
	)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.memory,
		params.iterations,
		params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func verifyPassword(encoded, password string) (bool, error) {
	params, salt, key, err := decodeArgon2id(encoded)
	if err != nil {
		return false, err
	}
	computed := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		//nolint:gosec // G115: len(key) is the decoded 32-byte hash length, far within uint32.
		uint32(len(key)),
	)

	return subtle.ConstantTimeCompare(computed, key) == 1, nil
}

func decodeArgon2id(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	var params argon2Params
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.memory,
		&params.iterations,
		&params.parallelism,
	); err != nil {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	return params, salt, key, nil
}
