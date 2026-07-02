package yacyproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yacyproto"
)

func TestIndexRequestFormRoundTrip(t *testing.T) {
	req := yacyproto.IndexRequest{
		NetworkName: "freeworld",
		Object:      yacyproto.IndexObjectHost,
	}

	got, err := yacyproto.ParseIndexRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatal(err)
	}

	if got != req {
		t.Fatalf("request = %+v, want %+v", got, req)
	}
}

func TestIndexRequestDefaultsToEmptyValues(t *testing.T) {
	got, err := yacyproto.ParseIndexRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if got.NetworkName != "" {
		t.Fatalf("NetworkName = %q, want empty", got.NetworkName)
	}
	if got.Object != "" {
		t.Fatalf("Object = %q, want empty", got.Object)
	}
}
