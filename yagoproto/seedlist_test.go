package yagoproto_test

import (
	"math"
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
	if req.IDPresent || req.NamePresent || req.CallbackPresent || req.PeerNamePresent {
		t.Fatalf("selector presence = %#v", req)
	}
	if _, ok := req.MaxCount.Get(); ok {
		t.Fatal("MaxCount present")
	}
	if minimumVersion, ok := req.MinVersion.Get(); !ok || minimumVersion != 0 {
		t.Fatalf("MinVersion = %v, %v; want 0, true", minimumVersion, ok)
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
	if !ok || minVersion != float64(float32(1.8)) {
		t.Fatalf("MinVersion = %v, %v; want float32 1.8, true", minVersion, ok)
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
	if !req.IDPresent || !req.NamePresent || !req.CallbackPresent || !req.PeerNamePresent {
		t.Fatalf("selector presence = %#v", req)
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
	if !ok || minVersion != float64(float32(1.9)) {
		t.Fatalf("MinVersion = %v, %v; want float32 1.9, true", minVersion, ok)
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

func TestSeedlistRequestFormPreservesBareIDSelector(t *testing.T) {
	t.Parallel()

	form := (yagoproto.SeedlistRequest{
		IncludeSelf: true,
		IDPresent:   true,
	}).Form()
	if !form.Has(yagoproto.FieldSeedlistID) || form.Get(yagoproto.FieldSeedlistID) != "" {
		t.Fatalf("form = %v", form)
	}
}

func TestSeedlistRequestUsesUpstreamMaxCountFallbacks(t *testing.T) {
	malformedMax, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMaxCount: {"many"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := malformedMax.MaxCount.Get(); present {
		t.Fatal("malformed maxcount did not fall back to the default")
	}

	overflowMax, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMaxCount: {"999999999999999999999999"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := overflowMax.MaxCount.Get(); present {
		t.Fatal("overflowing maxcount did not fall back to the default")
	}
	negativeOverflow, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMaxCount: {"-2147483649"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := negativeOverflow.MaxCount.Get(); present {
		t.Fatal("signed 32-bit maxcount overflow did not fall back to the default")
	}

	cappedMax, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMaxCount: {"1001"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	maxCount, present := cappedMax.MaxCount.Get()
	if !present || maxCount != yagoproto.SeedlistMaximumEntries {
		t.Fatalf("capped maxcount = %d, %v", maxCount, present)
	}
}

func TestSeedlistRequestUsesJavaSignedDecimalInt32MaxCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   string
		want    int
		present bool
	}{
		{value: "١٢٣", want: 123, present: true},
		{value: "４２", want: 42, present: true},
		{value: "+2147483647", want: yagoproto.SeedlistMaximumEntries, present: true},
		{value: "-2147483648", want: -2147483648, present: true},
		{value: "-2147483649", present: false},
		{value: "𝟙", present: false},
	}
	for _, test := range tests {
		request, err := yagoproto.ParseSeedlistRequest(
			t.Context(),
			url.Values{yagoproto.FieldSeedlistMaxCount: {test.value}},
		)
		if err != nil {
			t.Fatalf("ParseSeedlistRequest maxcount %q: %v", test.value, err)
		}
		got, present := request.MaxCount.Get()
		if got != test.want || present != test.present {
			t.Fatalf(
				"MaxCount for %q = %d, %v; want %d, %v",
				test.value,
				got,
				present,
				test.want,
				test.present,
			)
		}
	}
}

func TestSeedlistRequestUsesUpstreamMinVersionFallbacks(t *testing.T) {
	malformedVersion, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"many"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present := malformedVersion.MinVersion.Get()
	if !present || minimumVersion != 0 {
		t.Fatalf("malformed minversion = %v, %v; want 0, true", minimumVersion, present)
	}
	roundedVersion, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"0.1100244"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present = roundedVersion.MinVersion.Get()
	if !present || minimumVersion != float64(float32(0.1100244)) {
		t.Fatalf("float32 minversion = %v, %v", minimumVersion, present)
	}
	nanVersion, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"NaN"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present = nanVersion.MinVersion.Get()
	if !present || !math.IsNaN(minimumVersion) {
		t.Fatalf("NaN minversion = %v, %v", minimumVersion, present)
	}
	infiniteVersion, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"Infinity"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present = infiniteVersion.MinVersion.Get()
	if !present || minimumVersion != yagoproto.SeedlistMaximumEntries {
		t.Fatalf("infinite minversion = %v, %v", minimumVersion, present)
	}
	javaLiteralVersion, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"\x1f 1.5f \x00"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present = javaLiteralVersion.MinVersion.Get()
	if !present || minimumVersion != 1.5 {
		t.Fatalf("Java float literal minversion = %v, %v", minimumVersion, present)
	}
}

func TestSeedlistRequestDefaultsInvalidJavaMinVersion(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"nan", "+infinity", "Inf", "1_5", "NaNf", "Infinityd"} {
		request, parseErr := yagoproto.ParseSeedlistRequest(
			t.Context(),
			url.Values{yagoproto.FieldSeedlistMinVersion: {invalid}},
		)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		minimumVersion, parsed := request.MinVersion.Get()
		if !parsed || minimumVersion != 0 {
			t.Fatalf(
				"invalid Java minversion %q = %v, %v; want 0, true",
				invalid,
				minimumVersion,
				parsed,
			)
		}
	}
}

func TestSeedlistRequestAcceptsNegativeInfinityVersion(t *testing.T) {
	t.Parallel()

	request, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMinVersion: {"-Infinity"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	minimumVersion, present := request.MinVersion.Get()
	if !present || !math.IsInf(minimumVersion, -1) {
		t.Fatalf("negative infinite minversion = %v, %v", minimumVersion, present)
	}
}

func TestSeedlistRequestUsesUpstreamBooleanFallbacks(t *testing.T) {
	falseBooleans, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{
			yagoproto.FieldSeedlistMe:      {""},
			yagoproto.FieldSeedlistNode:    {"perhaps"},
			yagoproto.FieldSeedlistAddress: {"off"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if falseBooleans.IncludeSelf || falseBooleans.NodeOnly || falseBooleans.AddressOnly {
		t.Fatalf("false booleans = %#v", falseBooleans)
	}

	trueBooleans, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{
			yagoproto.FieldSeedlistMe:      {"on"},
			yagoproto.FieldSeedlistNode:    {"1"},
			yagoproto.FieldSeedlistAddress: {"TRUE"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !trueBooleans.IncludeSelf || !trueBooleans.NodeOnly || !trueBooleans.AddressOnly {
		t.Fatalf("true booleans = %#v", trueBooleans)
	}
}

func TestSeedlistRequestUsesUpstreamSelectorFallbacks(t *testing.T) {
	// "my" follows YaCy's containsKey semantics: any value — even one that is
	// not a boolean — selects the own seed.
	quirk, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMy: {"perhaps"}},
	)
	if err != nil || !quirk.OwnSeedOnly {
		t.Fatalf("my=perhaps: req=%#v err=%v, want OwnSeedOnly", quirk, err)
	}
	bare, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistMy: {""}},
	)
	if err != nil || !bare.OwnSeedOnly {
		t.Fatalf("bare ?my: req=%#v err=%v, want OwnSeedOnly", bare, err)
	}

	malformedID, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistID: {"short"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !malformedID.IDPresent {
		t.Fatal("malformed id key presence was lost")
	}
	if _, present := malformedID.ID.Get(); present {
		t.Fatal("malformed id was parsed as a hash")
	}

	bareID, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{yagoproto.FieldSeedlistID: {""}},
	)
	if err != nil || !bareID.IDPresent {
		t.Fatalf("bare id = %#v, %v", bareID, err)
	}

	bareSelectors, err := yagoproto.ParseSeedlistRequest(
		t.Context(),
		url.Values{
			yagoproto.FieldSeedlistName:     {""},
			yagoproto.FieldSeedlistPeerName: {""},
			yagoproto.FieldSeedlistCallback: {""},
		},
	)
	if err != nil || !bareSelectors.NamePresent || !bareSelectors.PeerNamePresent ||
		!bareSelectors.CallbackPresent {
		t.Fatalf("bare selectors = %#v, %v", bareSelectors, err)
	}
}
