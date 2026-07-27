package adminauth

import (
	"errors"
	"strings"
	"testing"
)

// A stored credential can be truncated by a partial write, a hand-edited vault,
// or a restore from a damaged backup. Well-formed base64 is not a well-formed
// credential: an empty key segment decodes cleanly and then asks the key
// derivation for a zero-length result, which x/crypto answers by writing
// through a nil BLAKE2b digest. A corrupted record has to be refused, because
// the alternative is that any login attempt against it crashes the node.
func TestDecodeRejectsAHashThatWouldCrashTheDerivation(t *testing.T) {
	t.Parallel()

	for _, item := range []struct {
		name    string
		encoded string
	}{
		{name: "empty key segment", encoded: "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$"},
		{
			name:    "short key",
			encoded: "$argon2id$v=19$m=65536,t=3,p=2$" + strings.Repeat("A", 22) + "$QUJD",
		},
		{name: "empty salt segment", encoded: "$argon2id$v=19$m=65536,t=3,p=2$$" + strings.Repeat("A", 43)},
		{
			name:    "zero parallelism",
			encoded: "$argon2id$v=19$m=65536,t=3,p=0$" + strings.Repeat("A", 22) + "$" + strings.Repeat("A", 43),
		},
		{
			name:    "zero iterations",
			encoded: "$argon2id$v=19$m=65536,t=0,p=2$" + strings.Repeat("A", 22) + "$" + strings.Repeat("A", 43),
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()

			if _, _, _, err := decodeArgon2id(item.encoded); !errors.Is(err, errMalformedHash) {
				t.Fatalf("decode error = %v, want a malformed-hash refusal", err)
			}
			// The refusal has to happen in the decoder: reaching the derivation
			// with these values is the crash itself.
			matched, err := verifyPassword(item.encoded, "any password")
			if matched || !errors.Is(err, errMalformedHash) {
				t.Fatalf("verify matched=%v error=%v, want a malformed-hash refusal", matched, err)
			}
		})
	}
}

// The guard must refuse damage, not credentials. A hash this package produced
// still decodes and still verifies its own password.
func TestDecodeKeepsAcceptingTheHashesThisPackageWrites(t *testing.T) {
	t.Parallel()

	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	params, salt, key, err := decodeArgon2id(encoded)
	if err != nil {
		t.Fatalf("decode own hash: %v", err)
	}
	if len(salt) != argonSaltLength || len(key) != argonKeyLength {
		t.Fatalf(
			"salt=%d key=%d, want %d and %d",
			len(salt),
			len(key),
			argonSaltLength,
			argonKeyLength,
		)
	}
	if params.memory != argonMemoryKiB || params.iterations != argonIterations ||
		params.parallelism != argonParallelism {
		t.Fatalf("params = %+v", params)
	}
	matched, err := verifyPassword(encoded, "correct horse battery staple")
	if err != nil || !matched {
		t.Fatalf("verify own password matched=%v error=%v", matched, err)
	}
	matched, err = verifyPassword(encoded, "wrong password")
	if err != nil || matched {
		t.Fatalf("wrong password matched=%v error=%v", matched, err)
	}
}
