package yagoproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func TestSeedlistRequestDefaultsToIncludingSelf(t *testing.T) {
	req, err := yagoproto.ParseSeedlistRequest(t.Context(), url.Values{})
	if err != nil {
		t.Fatal(err)
	}

	if !req.IncludeSelf {
		t.Fatal("IncludeSelf = false, want true")
	}
	if req.OwnSeedOnly {
		t.Fatal("OwnSeedOnly = true, want false")
	}
	if req.NodeOnly {
		t.Fatal("NodeOnly = true, want false")
	}
	if req.AddressOnly {
		t.Fatal("AddressOnly = true, want false")
	}
	if _, ok := req.MaxCount.Get(); ok {
		t.Fatal("MaxCount present")
	}
	if _, ok := req.MinVersion.Get(); ok {
		t.Fatal("MinVersion present")
	}
}

func TestSeedlistRequestParsesFilters(t *testing.T) {
	id := yagomodel.WordHash("peer")
	form := url.Values{
		yagoproto.FieldSeedlistMaxCount:   {"3"},
		yagoproto.FieldSeedlistMinVersion: {"1.8"},
		yagoproto.FieldSeedlistNode:       {"true"},
		yagoproto.FieldSeedlistMe:         {"false"},
		yagoproto.FieldSeedlistMy:         {"true"},
		yagoproto.FieldSeedlistID:         {id.String()},
		yagoproto.FieldSeedlistName:       {"peer-a"},
		yagoproto.FieldSeedlistAddress:    {"true"},
		yagoproto.FieldSeedlistCallback:   {"seedlist"},
		yagoproto.FieldSeedlistPeerName:   {"peer-b"},
	}

	req, err := yagoproto.ParseSeedlistRequest(t.Context(), form)
	if err != nil {
		t.Fatal(err)
	}

	maxCount, ok := req.MaxCount.Get()
	if !ok || maxCount != 3 {
		t.Fatalf("MaxCount = %d, %v; want 3, true", maxCount, ok)
	}
	minVersion, ok := req.MinVersion.Get()
	if !ok || minVersion != 1.8 {
		t.Fatalf("MinVersion = %v, %v; want 1.8, true", minVersion, ok)
	}
	parsedID, ok := req.ID.Get()
	if !ok || parsedID != id {
		t.Fatalf("ID = %q, %v; want %q, true", parsedID, ok, id)
	}
	if req.IncludeSelf {
		t.Fatal("IncludeSelf = true, want false")
	}
	if !req.OwnSeedOnly {
		t.Fatal("OwnSeedOnly = false, want true")
	}
	if !req.NodeOnly {
		t.Fatal("NodeOnly = false, want true")
	}
	if !req.AddressOnly {
		t.Fatal("AddressOnly = false, want true")
	}
	if req.Name != "peer-a" {
		t.Fatalf("Name = %q, want peer-a", req.Name)
	}
	if req.Callback != "seedlist" {
		t.Fatalf("Callback = %q, want seedlist", req.Callback)
	}
	if req.PeerName != "peer-b" {
		t.Fatalf("PeerName = %q, want peer-b", req.PeerName)
	}
}

func TestSeedlistRequestFormRoundTrip(t *testing.T) {
	id := yagomodel.WordHash("peer")
	original := yagoproto.SeedlistRequest{
		MaxCount:    yagomodel.Some(4),
		MinVersion:  yagomodel.Some(1.9),
		NodeOnly:    true,
		IncludeSelf: false,
		OwnSeedOnly: true,
		ID:          yagomodel.Some(id),
		Name:        "peer-a",
		AddressOnly: true,
		Callback:    "seedlist",
		PeerName:    "peer-b",
	}

	parsed, err := yagoproto.ParseSeedlistRequest(t.Context(), original.Form())
	if err != nil {
		t.Fatal(err)
	}

	maxCount, ok := parsed.MaxCount.Get()
	if !ok || maxCount != 4 {
		t.Fatalf("MaxCount = %d, %v; want 4, true", maxCount, ok)
	}
	minVersion, ok := parsed.MinVersion.Get()
	if !ok || minVersion != 1.9 {
		t.Fatalf("MinVersion = %v, %v; want 1.9, true", minVersion, ok)
	}
	parsedID, ok := parsed.ID.Get()
	if !ok || parsedID != id {
		t.Fatalf("ID = %q, %v; want %q, true", parsedID, ok, id)
	}
	if parsed.IncludeSelf || !parsed.OwnSeedOnly || !parsed.NodeOnly ||
		!parsed.AddressOnly || parsed.Name != "peer-a" ||
		parsed.Callback != "seedlist" || parsed.PeerName != "peer-b" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestSeedlistRequestRejectsBadFields(t *testing.T) {
	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMaxCount: {"many"}},
	); err == nil {
		t.Fatal("expected bad maxcount error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"many"}},
	); err == nil {
		t.Fatal("expected bad minversion error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMe: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad me error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistNode: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad node error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMy: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad my error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistAddress: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad address error")
	}

	if _, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistID: {"short"}},
	); err == nil {
		t.Fatal("expected bad id error")
	}
}
