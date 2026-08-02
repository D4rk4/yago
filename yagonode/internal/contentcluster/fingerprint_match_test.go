package contentcluster

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// TestFingerprintMatchMirrorsTheRecord holds the reduced shape against the
// stored one. Both decode the same bytes, so a JSON tag renamed on one and not
// the other does not fail -- it degrades, silently reading a zero where the
// record has a value, which for the content hash would drop every member of an
// exact posting bucket. Marshalling the record and decoding it as a match is
// the only check that sees that.
func TestFingerprintMatchMirrorsTheRecord(t *testing.T) {
	t.Parallel()

	for _, record := range []fingerprintRecord{
		{
			URL:                "https://populated.example",
			ContentHash:        "9f2c1e8b",
			Fingerprint:        0x0123456789abcdef,
			Shingles:           []uint64{11, 22, 33},
			ClusterID:          "cluster",
			CanonicalPreferred: true,
			Quality:            0.5,
			InboundAuthority:   0.25,
		},
		// A record whose shingles were never built: the vector is nil, and the
		// band rule has to read that as absent.
		{URL: "https://bare.example", ContentHash: "hash", Fingerprint: 7},
		// An empty-but-present vector must read the same as a nil one, because
		// len() cannot tell them apart either.
		{URL: "https://empty.example", ContentHash: "hash", Shingles: []uint64{}},
	} {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		var match fingerprintMatch
		if err := json.Unmarshal(raw, &match); err != nil {
			t.Fatalf("decode match: %v", err)
		}
		if match.ContentHash != record.ContentHash {
			t.Fatalf("content hash = %q, want %q", match.ContentHash, record.ContentHash)
		}
		if match.Fingerprint != record.Fingerprint {
			t.Fatalf("fingerprint = %d, want %d", match.Fingerprint, record.Fingerprint)
		}
		if want := len(record.Shingles) > 0; bool(match.HasShingles) != want {
			t.Fatalf("shingle presence = %v, want %v", bool(match.HasShingles), want)
		}
	}
}

// TestFingerprintTransitionMatchMirrorsTheTransition does the same for the
// transition entry, whose identity fields decide whether a projected read is
// honoured at all.
func TestFingerprintTransitionMatchMirrorsTheTransition(t *testing.T) {
	t.Parallel()

	transition := fingerprintTransition{
		Token:         "token",
		URL:           "https://transition.example",
		Previous:      fingerprintRecord{URL: "https://transition.example", Shingles: []uint64{9}},
		PreviousFound: true,
		Current: fingerprintRecord{
			URL:         "https://transition.example",
			ContentHash: "current-hash",
			Fingerprint: 42,
			Shingles:    []uint64{1, 2},
		},
		CurrentFound: true,
	}
	raw, err := json.Marshal(transition)
	if err != nil {
		t.Fatalf("marshal transition: %v", err)
	}
	var reduced fingerprintTransitionMatch
	if err := json.Unmarshal(raw, &reduced); err != nil {
		t.Fatalf("decode reduced transition: %v", err)
	}
	if reduced.Token != transition.Token || reduced.URL != transition.URL {
		t.Fatalf("identity = %q/%q, want %q/%q",
			reduced.URL, reduced.Token, transition.URL, transition.Token)
	}
	if reduced.CurrentFound != transition.CurrentFound {
		t.Fatalf("current found = %v, want %v", reduced.CurrentFound, transition.CurrentFound)
	}
	if reduced.Current.ContentHash != transition.Current.ContentHash ||
		reduced.Current.Fingerprint != transition.Current.Fingerprint ||
		!reduced.Current.HasShingles {
		t.Fatalf("current = %+v, want it to mirror %+v", reduced.Current, transition.Current)
	}
}

func TestShinglePresenceReadsWithoutBuildingTheVector(t *testing.T) {
	t.Parallel()

	for _, item := range []struct {
		name    string
		encoded string
		want    bool
		refuse  bool
	}{
		{name: "absent key", encoded: `{}`},
		{name: "null", encoded: `{"shingles":null}`},
		{name: "empty", encoded: `{"shingles":[]}`},
		{name: "empty with space", encoded: `{"shingles":[ ]}`},
		{name: "one value", encoded: `{"shingles":[1]}`, want: true},
		{name: "many values", encoded: `{"shingles":[1,2,3]}`, want: true},
		// A value that could not have decoded into []uint64 must not decode
		// here either, or a corrupt record would pass the posting rule instead
		// of failing the read.
		{name: "string", encoded: `{"shingles":"nope"}`, refuse: true},
		{name: "number", encoded: `{"shingles":7}`, refuse: true},
		{name: "object", encoded: `{"shingles":{}}`, refuse: true},
	} {
		var match fingerprintMatch
		err := json.Unmarshal([]byte(item.encoded), &match)
		if item.refuse {
			if err == nil {
				t.Fatalf("%s: decoded a value that is not a shingle array", item.name)
			}

			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		if bool(match.HasShingles) != item.want {
			t.Fatalf("%s: presence = %v, want %v", item.name, bool(match.HasShingles), item.want)
		}
	}
}

// TestFingerprintMatchDecodeDoesNotScaleWithShingles pins the property the
// reduced shape exists for. Reading a member of a posting bucket must cost the
// same whether that member carries four shingles or four thousand, because a
// page is written by re-reading every member of every bucket it belongs to. If
// the presence field is ever changed back to something that materializes the
// vector, the allocation count starts tracking its length and this fails.
// Not parallel: testing.AllocsPerRun refuses to run alongside other tests.
func TestFingerprintMatchDecodeDoesNotScaleWithShingles(t *testing.T) {
	encode := func(count int) []byte {
		shingles := make([]uint64, count)
		for i := range shingles {
			shingles[i] = uint64(i) * 2654435761
		}
		raw, err := json.Marshal(fingerprintRecord{
			URL:         "https://scaling.example",
			ContentHash: "hash",
			Fingerprint: 7,
			Shingles:    shingles,
		})
		if err != nil {
			t.Fatalf("marshal %d shingles: %v", count, err)
		}

		return raw
	}
	allocations := func(raw []byte) float64 {
		return testing.AllocsPerRun(20, func() {
			var match fingerprintMatch
			if err := json.Unmarshal(raw, &match); err != nil {
				t.Fatalf("decode: %v", err)
			}
		})
	}

	small, large := encode(4), encode(4096)
	if len(large) < 10*len(small) {
		t.Fatalf("the large payload is not large enough to be a test: %d vs %d bytes",
			len(large), len(small))
	}
	// A tolerance rather than equality: the decoder may take an extra internal
	// allocation on the longer input without ever building the vector, and the
	// race detector shifts absolute counts. What must not happen is growth in
	// proportion to the thousandfold difference in shingle count.
	if allocations(large) > allocations(small)+2 {
		t.Fatalf(
			"decoding %d shingles allocated %.0f against %.0f for 4 -- the vector is being built",
			4096,
			allocations(large),
			allocations(small),
		)
	}
}

// TestReducedShapesRefuseACorruptTransition keeps the reduced transition read on
// the same contract as the full one: a stored entry whose identity does not
// match the key it was read under is a refusal, not a silent miss.
func TestReducedShapesRefuseACorruptTransition(t *testing.T) {
	t.Parallel()

	index, engine := openFaultIndex(t, Limits{})
	url := "https://reduced-identity.example"
	raw, err := json.Marshal(fingerprintTransition{Token: "token", URL: "https://other.example"})
	if err != nil {
		t.Fatal(err)
	}
	engine.putRaw(fingerprintBucketName, transitionKey(url), raw)
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, _, readErr := index.projectedFingerprintMatch(tx, url)

		return readErr
	})
	if err == nil || !strings.Contains(err.Error(), "identity is invalid") {
		t.Fatalf("reduced transition identity error = %v", err)
	}
}

// TestRawJSONCodecKeepsTheCallerContract pins the two properties the fingerprint
// keyspace relies on after it stopped routing reads through jsonCodec: bytes
// survive a round trip unchanged, and a decoded value does not alias the buffer
// it came from. The second one matters because the storage engine may hand back
// a window into its mapping that is only valid inside the transaction, and the
// codec this replaced copied. Returning the window itself would leave callers
// holding memory that is reused once the transaction ends -- a corruption that
// no round-trip test can see.
func TestRawJSONCodecKeepsTheCallerContract(t *testing.T) {
	t.Parallel()

	document := []byte(`{"url":"https://codec.example","shingles":[1,2,3]}`)
	source := append([]byte(nil), document...)

	decoded, err := (rawJSONCodec{}).Decode(source)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != string(document) {
		t.Fatalf("decoded = %s, want %s", decoded, document)
	}
	// Overwrite the buffer the way a reused transaction mapping would.
	for i := range source {
		source[i] = 'x'
	}
	if string(decoded) != string(document) {
		t.Fatalf("decoded value aliases its source: %s", decoded)
	}

	encoded, err := (rawJSONCodec{}).Encode(document)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != string(document) {
		t.Fatalf("encoded = %s, want the document unchanged", encoded)
	}
}

// TestFingerprintReadsReportStorageFailures covers the branch that a corrupt
// stored document used to reach through the JSON codec. With the passthrough
// codec a bad value no longer fails inside the vault, so the only remaining way
// into it is the storage engine refusing the read -- which is what the real one
// does for a checksum mismatch, an unknown format tag, or a value that will not
// decompress. Both the record read and the transition read have to report it.
func TestFingerprintReadsReportStorageFailures(t *testing.T) {
	t.Parallel()

	for _, item := range []struct {
		name string
		read func(*Index, *vault.Txn) error
	}{
		{
			name: "published record",
			read: func(index *Index, tx *vault.Txn) error {
				_, _, err := index.publishedFingerprint(tx, "https://failing.example")

				return err
			},
		},
		{
			name: "reduced record",
			read: func(index *Index, tx *vault.Txn) error {
				_, _, err := index.fingerprints.match(tx, vault.Key("https://failing.example"))

				return err
			},
		},
		{
			name: "transition",
			read: func(index *Index, tx *vault.Txn) error {
				_, _, err := index.fingerprints.transition(tx, "https://failing.example")

				return err
			},
		},
		{
			name: "reduced transition",
			read: func(index *Index, tx *vault.Txn) error {
				_, _, err := index.fingerprints.transitionMatch(tx, "https://failing.example")

				return err
			},
		},
	} {
		index, engine := openFaultIndex(t, Limits{})
		engine.readFailure = fingerprintBucketName
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			return item.read(index, tx)
		})
		if !errors.Is(err, errInjectedClusterVault) {
			t.Fatalf("%s read error = %v, want the injected storage failure", item.name, err)
		}
	}
}
