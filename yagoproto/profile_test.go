package yagoproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestProfileRequestFormRoundTrip(t *testing.T) {
	req := yagoproto.ProfileRequest{NetworkName: "freeworld"}

	got, err := yagoproto.ParseProfileRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatal(err)
	}

	if got != req {
		t.Fatalf("request = %+v, want %+v", got, req)
	}
}

func TestProfileRequestDefaultsToEmptyNetwork(t *testing.T) {
	got, err := yagoproto.ParseProfileRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if got.NetworkName != "" {
		t.Fatalf("NetworkName = %q, want empty", got.NetworkName)
	}
}
