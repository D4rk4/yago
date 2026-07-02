package yacyproto_test

import (
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

func TestSeedlistRequestDefaultsToIncludingSelf(t *testing.T) {
	req, err := yacyproto.ParseSeedlistRequest(t.Context(), url.Values{})
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
	id := yacymodel.WordHash("peer")
	form := url.Values{
		yacyproto.FieldSeedlistMaxCount:   {"3"},
		yacyproto.FieldSeedlistMinVersion: {"1.8"},
		yacyproto.FieldSeedlistNode:       {"true"},
		yacyproto.FieldSeedlistMe:         {"false"},
		yacyproto.FieldSeedlistMy:         {"true"},
		yacyproto.FieldSeedlistID:         {id.String()},
		yacyproto.FieldSeedlistName:       {"peer-a"},
		yacyproto.FieldSeedlistAddress:    {"true"},
		yacyproto.FieldSeedlistCallback:   {"seedlist"},
		yacyproto.FieldSeedlistPeerName:   {"peer-b"},
	}

	req, err := yacyproto.ParseSeedlistRequest(t.Context(), form)
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
	id := yacymodel.WordHash("peer")
	original := yacyproto.SeedlistRequest{
		MaxCount:    yacymodel.Some(4),
		MinVersion:  yacymodel.Some(1.9),
		NodeOnly:    true,
		IncludeSelf: false,
		OwnSeedOnly: true,
		ID:          yacymodel.Some(id),
		Name:        "peer-a",
		AddressOnly: true,
		Callback:    "seedlist",
		PeerName:    "peer-b",
	}

	parsed, err := yacyproto.ParseSeedlistRequest(t.Context(), original.Form())
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
	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistMaxCount: {"many"}},
	); err == nil {
		t.Fatal("expected bad maxcount error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistMinVersion: {"many"}},
	); err == nil {
		t.Fatal("expected bad minversion error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistMe: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad me error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistNode: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad node error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistMy: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad my error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistAddress: {"perhaps"}},
	); err == nil {
		t.Fatal("expected bad address error")
	}

	if _, err := yacyproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yacyproto.FieldSeedlistID: {"short"}},
	); err == nil {
		t.Fatal("expected bad id error")
	}
}
