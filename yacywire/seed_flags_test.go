package yacywire

import (
	"errors"
	"testing"
)

func TestZeroFlags(t *testing.T) {
	f := ZeroFlags()
	if f.String() != FlagsZero {
		t.Errorf("ZeroFlags() = %q, want %q", f, FlagsZero)
	}
	for _, bit := range []int{
		FlagDirectConnect, FlagAcceptRemoteCrawl, FlagAcceptRemoteIndex,
		FlagRootNode, FlagSSLAvailable,
	} {
		if f.Get(bit) {
			t.Errorf("zero flags: bit %d set", bit)
		}
	}
}

func TestFlagsSetGet(t *testing.T) {
	f := ZeroFlags().Set(FlagAcceptRemoteIndex, true)
	if !f.Get(FlagAcceptRemoteIndex) {
		t.Error("expected FlagAcceptRemoteIndex set")
	}
	if f.Get(FlagDirectConnect) {
		t.Error("unexpected FlagDirectConnect set")
	}
	if len(f.String()) != FlagsLength {
		t.Errorf("flags width = %d, want %d", len(f.String()), FlagsLength)
	}
	if f[0] != ' '|(1<<FlagAcceptRemoteIndex) {
		t.Errorf("atom 0 = %#x", f[0])
	}
	f = f.Set(FlagAcceptRemoteIndex, false)
	if f.Get(FlagAcceptRemoteIndex) {
		t.Error("expected FlagAcceptRemoteIndex cleared")
	}
}

func TestParseFlags(t *testing.T) {
	if _, err := ParseFlags("   "); !errors.Is(err, ErrInvalidFlags) {
		t.Fatalf("ParseFlags short = %v, want ErrInvalidFlags", err)
	}
	f, err := ParseFlags(FlagsZero)
	if err != nil {
		t.Fatal(err)
	}
	if f != ZeroFlags() {
		t.Error("ParseFlags(FlagsZero) != ZeroFlags()")
	}
}
