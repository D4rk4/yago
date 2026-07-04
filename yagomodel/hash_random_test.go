package yagomodel_test

import (
	"bytes"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestGenerateHashIsValidAndDeterministicPerEntropy(t *testing.T) {
	entropy := bytes.Repeat([]byte{0xAB}, 64)

	hash, err := yagomodel.GenerateHash(bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("generate hash: %v", err)
	}
	if len(hash) != yagomodel.HashLength {
		t.Fatalf("length = %d, want %d", len(hash), yagomodel.HashLength)
	}
	if !hash.Valid() {
		t.Fatalf("hash %q is not valid", hash)
	}

	again, err := yagomodel.GenerateHash(bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("generate hash again: %v", err)
	}
	if hash != again {
		t.Fatalf("non-deterministic: %q != %q", hash, again)
	}
}

func TestGenerateHashReturnsErrorOnShortEntropy(t *testing.T) {
	if _, err := yagomodel.GenerateHash(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error for insufficient entropy")
	}
}

func TestNewHashProducesValidHash(t *testing.T) {
	hash, err := yagomodel.NewHash()
	if err != nil {
		t.Fatalf("new hash: %v", err)
	}
	if !hash.Valid() {
		t.Fatalf("hash %q is not valid", hash)
	}
}
