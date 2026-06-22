package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestRunPrintsValidPeerHash(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32))); err != nil {
		t.Fatalf("run: %v", err)
	}

	hash, err := yacymodel.ParseHash(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatalf("parse printed hash: %v", err)
	}
	if !hash.Valid() {
		t.Fatalf("hash %q is not valid", hash)
	}
}

func TestRunReturnsErrorOnShortEntropy(t *testing.T) {
	if err := run(&bytes.Buffer{}, bytes.NewReader(nil)); err == nil {
		t.Fatal("expected error for insufficient entropy")
	}
}
