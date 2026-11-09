package yagopdf

import (
	"bytes"
	"os"
	"testing"
)

func syntheticBinaryNoise(t *testing.T, length int) []byte {
	t.Helper()
	seed, err := os.ReadFile("testdata/binary-noise.bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != 4096 {
		t.Fatalf("binary fixture bytes = %d, want 4096", len(seed))
	}
	return bytes.Repeat(seed, (length+len(seed)-1)/len(seed))[:length]
}
