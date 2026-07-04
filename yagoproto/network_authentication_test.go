package yagoproto_test

import (
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestNetworkUnitDefaultsToFreeworld(t *testing.T) {
	t.Parallel()

	if got := yagoproto.NetworkUnit(""); got != yagoproto.DefaultNetwork {
		t.Fatalf("NetworkUnit(\"\") = %q, want %q", got, yagoproto.DefaultNetwork)
	}

	if got := yagoproto.NetworkUnit("intranet"); got != "intranet" {
		t.Fatalf("NetworkUnit(\"intranet\") = %q, want intranet", got)
	}
}

func TestMagicMD5IsStableHexDigest(t *testing.T) {
	t.Parallel()

	got := yagoproto.MagicMD5("key", "iam", "essentials")
	want := "153e7e6a5187ecdb2fad0620f968e6c4"

	if got != want {
		t.Fatalf("MagicMD5 = %q, want %q", got, want)
	}

	if got == yagoproto.MagicMD5("key", "iam", "other") {
		t.Fatal("MagicMD5 must depend on essentials")
	}
}
