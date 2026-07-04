package yagoproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

func TestListRequestFormRoundTrip(t *testing.T) {
	req := yagoproto.ListRequest{
		NetworkName: yagoproto.DefaultNetwork,
		Column:      yagoproto.ListColumnBlack,
		Name:        "url.default.black",
	}

	got, err := yagoproto.ParseListRequest(t.Context(), req.Form())
	if err != nil {
		t.Fatal(err)
	}

	if got != req {
		t.Fatalf("request = %+v, want %+v", got, req)
	}
}

func TestListRequestDefaultsToEmptyValues(t *testing.T) {
	got, err := yagoproto.ParseListRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if got.Column != "" {
		t.Fatalf("Column = %q, want empty", got.Column)
	}
	if got.NetworkName != "" {
		t.Fatalf("NetworkName = %q, want empty", got.NetworkName)
	}
	if got.Name != "" {
		t.Fatalf("Name = %q, want empty", got.Name)
	}
}
