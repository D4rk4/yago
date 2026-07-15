package corpussignals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/hostlinkgraph"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type rawCheckpointCodec struct{}

func (rawCheckpointCodec) Encode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

func (rawCheckpointCodec) Decode(value []byte) ([]byte, error) {
	return append([]byte(nil), value...), nil
}

type checkpointWriteFailureEngine struct {
	fail bool
}

func (e *checkpointWriteFailureEngine) Update(
	_ context.Context,
	operation func(vault.EngineTxn) error,
) error {
	return operation(checkpointWriteFailureTransaction{engine: e, writable: true})
}

func (e *checkpointWriteFailureEngine) View(
	_ context.Context,
	operation func(vault.EngineTxn) error,
) error {
	return operation(checkpointWriteFailureTransaction{engine: e})
}

func (*checkpointWriteFailureEngine) Provision(vault.Name) error { return nil }

func (*checkpointWriteFailureEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }

func (*checkpointWriteFailureEngine) QuotaBytes() int64 { return 0 }

func (*checkpointWriteFailureEngine) Close() error { return nil }

type checkpointWriteFailureTransaction struct {
	engine   *checkpointWriteFailureEngine
	writable bool
}

func (t checkpointWriteFailureTransaction) Bucket(vault.Name) vault.EngineBucket {
	return checkpointWriteFailureBucket{engine: t.engine}
}

func (t checkpointWriteFailureTransaction) Writable() bool { return t.writable }

type checkpointWriteFailureBucket struct {
	engine *checkpointWriteFailureEngine
}

func (checkpointWriteFailureBucket) Get(vault.Key) []byte { return nil }

func (b checkpointWriteFailureBucket) Put(vault.Key, []byte) error {
	if b.engine.fail {
		return errors.New("write failed")
	}

	return nil
}

func (checkpointWriteFailureBucket) Delete(vault.Key) error { return nil }

func (checkpointWriteFailureBucket) Scan(
	vault.Key,
	func(vault.Key, []byte) (bool, error),
) error {
	return nil
}

func validCheckpoint() Checkpoint {
	return Checkpoint{
		Authority: hostrank.AuthorityTable{
			"source.example": {Score: 0.25, Confidence: 0.5},
			"target.example": {Score: 1, Confidence: 1},
		},
		Citations: []hostrank.Citation{{
			SourceURL:  "https://source.example/page",
			TargetURL:  "https://target.example/page",
			Confidence: 1,
		}},
		Spelling:       map[string]int{"golang": 4},
		WordForms:      map[string]int{"черногория": 3, "черногории": 2},
		WordFormsReady: true,
		HostLinks: hostlinkgraph.Graph{
			RowDefinition: hostlinkgraph.HostReferenceRowDefinition,
			LinkedHosts: []hostlinkgraph.LinkedHost{{
				HostHash:   "target",
				References: []json.RawMessage{json.RawMessage(`{"h":"source"}`)},
			}},
		},
		HostLinksReady:       true,
		TrustDomains:         []string{"source.example"},
		TrustBlend:           0.25,
		CompletedAtUnixMilli: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func TestCheckpointRepositoryPersistsOwnedAtomicSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus-signals.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Replace(t.Context(), Checkpoint{}); err == nil {
		t.Fatal("invalid checkpoint replaced")
	}
	if checkpoint, found, loadErr := repository.Load(t.Context()); loadErr != nil || found ||
		!reflect.DeepEqual(checkpoint, cloneCheckpoint(Checkpoint{})) {
		t.Fatalf("empty load = %#v, %t, %v", checkpoint, found, loadErr)
	}
	want := validCheckpoint()
	input := cloneCheckpoint(want)
	if err := repository.Replace(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	input.Authority["target.example"] = hostrank.AuthorityEvidence{}
	input.Citations[0].SourceURL = "https://changed.example/"
	input.Spelling["golang"] = 1
	input.WordForms["черногория"] = 1
	input.HostLinks.LinkedHosts[0].HostHash = "change"
	input.HostLinks.LinkedHosts[0].References[0][2] = 'x'
	input.TrustDomains[0] = "changed.example"

	loaded, found, err := repository.Load(t.Context())
	if err != nil || !found || !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded checkpoint = %#v, %t, %v", loaded, found, err)
	}
	loaded.Authority["target.example"] = hostrank.AuthorityEvidence{}
	loaded.Citations[0].TargetURL = "https://changed.example/"
	loaded.Spelling["golang"] = 1
	loaded.WordForms["черногории"] = 1
	loaded.HostLinks.LinkedHosts[0].HostHash = "change"
	loaded.HostLinks.LinkedHosts[0].References[0][2] = 'x'
	loaded.TrustDomains[0] = "changed.example"
	reloaded, found, err := repository.Load(t.Context())
	if err != nil || !found || !reflect.DeepEqual(reloaded, want) {
		t.Fatalf("owned checkpoint = %#v, %t, %v", reloaded, found, err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStorage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedStorage.Close() })
	reopened, err := Open(reopenedStorage)
	if err != nil {
		t.Fatal(err)
	}
	persisted, found, err := reopened.Load(t.Context())
	if err != nil || !found || !reflect.DeepEqual(persisted, want) {
		t.Fatalf("persisted checkpoint = %#v, %t, %v", persisted, found, err)
	}
}

func TestCheckpointRepositoryReportsStorageFailures(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Fatal("nil storage opened")
	}
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := Open(storage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(storage); err == nil {
		t.Fatal("duplicate repository opened")
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Load(t.Context()); err == nil {
		t.Fatal("closed repository loaded")
	}
	if err := repository.Replace(t.Context(), validCheckpoint()); err == nil {
		t.Fatal("closed repository replaced")
	}

	engine := &checkpointWriteFailureEngine{}
	failingStorage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	failingRepository, err := Open(failingStorage)
	if err != nil {
		t.Fatal(err)
	}
	engine.fail = true
	if err := failingRepository.Replace(t.Context(), validCheckpoint()); err == nil {
		t.Fatal("failed checkpoint write succeeded")
	}
}

func TestCheckpointRepositoryRejectsCorruptStoredRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt-corpus-signals.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := vault.Register(storage, checkpointBucket, rawCheckpointCodec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return raw.Put(tx, checkpointKey, []byte("{"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStorage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedStorage.Close() })
	repository, err := Open(reopenedStorage)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Load(t.Context()); err == nil {
		t.Fatal("corrupt checkpoint loaded")
	}
}

func TestCheckpointCodecRoundTripsOwnedSnapshot(t *testing.T) {
	codec := checkpointCodec{}
	valid := checkpointRecord{Format: checkpointFormat, Checkpoint: validCheckpoint()}
	encoded, err := codec.Encode(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, valid) {
		t.Fatalf("round trip = %#v, %v", decoded, err)
	}
	decoded.Checkpoint.Spelling["golang"] = 1
	decodedAgain, err := codec.Decode(encoded)
	if err != nil || decodedAgain.Checkpoint.Spelling["golang"] != 4 {
		t.Fatalf("owned decode = %#v, %v", decodedAgain, err)
	}
}

func TestCheckpointCodecRejectsInvalidRecords(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*checkpointRecord)
	}{
		{name: "format", mutate: func(record *checkpointRecord) { record.Format = "future" }},
		{name: "missing", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Authority = nil
		}},
		{name: "collections", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Authority = make(
				hostrank.AuthorityTable,
				maximumCheckpointAuthorityDomains+1,
			)
			for index := 0; index <= maximumCheckpointAuthorityDomains; index++ {
				record.Checkpoint.Authority[fmt.Sprintf("%d.example", index)] = hostrank.AuthorityEvidence{}
			}
		}},
		{name: "unavailable forms", mutate: func(record *checkpointRecord) {
			record.Checkpoint.WordFormsReady = false
		}},
		{name: "completion", mutate: func(record *checkpointRecord) {
			record.Checkpoint.CompletedAtUnixMilli = 0
		}},
		{name: "trust blend negative", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustBlend = -0.1
		}},
		{name: "trust blend high", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustBlend = 1.1
		}},
		{name: "trust blend nan", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustBlend = math.NaN()
		}},
		{name: "trust blend infinite", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustBlend = math.Inf(1)
		}},
		{name: "authority domain", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Authority["UPPER.example"] = hostrank.AuthorityEvidence{}
		}},
		{name: "authority score", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Authority["target.example"] = hostrank.AuthorityEvidence{Score: -1}
		}},
		{name: "authority confidence", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Authority["target.example"] = hostrank.AuthorityEvidence{
				Score: 1, Confidence: math.NaN(),
			}
		}},
		{name: "citation size", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Citations[0].SourceURL = strings.Repeat(
				"a",
				maximumCheckpointURLBytes+1,
			)
		}},
		{name: "citation duplicate", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Citations = append(
				record.Checkpoint.Citations,
				record.Checkpoint.Citations[0],
			)
		}},
		{name: "spelling", mutate: func(record *checkpointRecord) {
			record.Checkpoint.Spelling = map[string]int{"": 1}
		}},
		{name: "word forms", mutate: func(record *checkpointRecord) {
			record.Checkpoint.WordForms = map[string]int{"word": 0}
		}},
		{name: "trust domain", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustDomains = []string{"UPPER.example"}
		}},
		{name: "duplicate trust domain", mutate: func(record *checkpointRecord) {
			record.Checkpoint.TrustDomains = []string{"source.example", "source.example"}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			record := checkpointRecord{Format: checkpointFormat, Checkpoint: validCheckpoint()}
			testCase.mutate(&record)
			if _, err := (checkpointCodec{}).Encode(record); err == nil {
				t.Fatalf("invalid record encoded: %#v", record)
			}
		})
	}
}

func TestCheckpointCodecEnforcesSerializedSize(t *testing.T) {
	checkpoint := validCheckpoint()
	checkpoint.Spelling = escapedVocabulary(maximumCheckpointSpellingTerms)
	checkpoint.WordForms = escapedVocabulary(maximumCheckpointWordFormTerms)
	if _, err := (checkpointCodec{}).Encode(checkpointRecord{
		Format: checkpointFormat, Checkpoint: checkpoint,
	}); err == nil {
		t.Fatal("oversized checkpoint encoded")
	}
}

func TestCheckpointCodecRejectsInvalidDocuments(t *testing.T) {
	codec := checkpointCodec{}
	if _, err := codec.Decode(nil); err == nil {
		t.Fatal("empty checkpoint decoded")
	}
	if _, err := codec.Decode(make([]byte, maximumCheckpointBytes+1)); err == nil {
		t.Fatal("oversized checkpoint decoded")
	}
	if _, err := codec.Decode([]byte("{")); err == nil {
		t.Fatal("malformed checkpoint decoded")
	}
	invalid, err := json.Marshal(checkpointRecord{
		Format: checkpointFormat,
		Checkpoint: Checkpoint{
			Authority: hostrank.AuthorityTable{}, Citations: []hostrank.Citation{},
			Spelling:  map[string]int{},
			WordForms: map[string]int{}, TrustDomains: []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(invalid); err == nil {
		t.Fatal("invalid checkpoint decoded")
	}
}

func escapedVocabulary(terms int) map[string]int {
	vocabulary := make(map[string]int, terms)
	for index := 0; index < terms; index++ {
		term := make([]byte, maximumCheckpointVocabularyTermBytes)
		term[62] = byte(index >> 10)
		term[63] = byte(index >> 5)
		term[64] = byte(index)
		vocabulary[string(term)] = 1
	}

	return vocabulary
}
