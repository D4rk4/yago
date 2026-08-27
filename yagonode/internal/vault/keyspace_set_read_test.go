package vault

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type scriptedSetReadBucket struct {
	*scriptedBucket
	values          [][]byte
	valuePresence   []bool
	valueFailure    error
	presence        []bool
	presenceFailure error
	valueReads      int
	presenceReads   int
}

func (b *scriptedSetReadBucket) ReadValues(
	context.Context,
	[]Key,
) ([][]byte, []bool, error) {
	b.valueReads++

	return b.values, b.valuePresence, b.valueFailure
}

func (b *scriptedSetReadBucket) ReadPresence(
	context.Context,
	[]Key,
) ([]bool, error) {
	b.presenceReads++

	return b.presence, b.presenceFailure
}

func TestKeyspaceSetReadUsesBatchCapabilities(t *testing.T) {
	bucket := &scriptedSetReadBucket{
		scriptedBucket: &scriptedBucket{},
		values:         [][]byte{[]byte("first"), nil, []byte("third")},
		valuePresence:  []bool{true, false, true},
		presence:       []bool{true, false, true},
	}
	keyspace := &Keyspace[string]{name: "data", codec: internalStringCodec{}}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	keys := []Key{Key("first"), Key("missing"), Key("third")}
	values, found, err := keyspace.Values(t.Context(), tx, keys)
	if err != nil || len(values) != 3 || values[0] != "first" || values[1] != "" ||
		values[2] != "third" || !found[0] || found[1] || !found[2] ||
		bucket.valueReads != 1 {
		t.Fatalf("values=%v found=%v reads=%d error=%v", values, found, bucket.valueReads, err)
	}
	presence, err := keyspace.Presence(t.Context(), tx, keys)
	if err != nil || len(presence) != 3 || !presence[0] || presence[1] ||
		!presence[2] || bucket.presenceReads != 1 {
		t.Fatalf("presence=%v reads=%d error=%v", presence, bucket.presenceReads, err)
	}
}

func TestKeyspaceSetReadFallbacksPreserveOrder(t *testing.T) {
	bucket := &scriptedBucket{values: map[string][]byte{
		"first": []byte("one"),
		"third": []byte("three"),
	}}
	keyspace := &Keyspace[string]{name: "data", codec: internalStringCodec{}}
	keys := []Key{Key("first"), Key("missing"), Key("third")}
	tx := &Txn{etx: presenceTxn{bucket: bucket}}
	values, found, err := keyspace.Values(t.Context(), tx, keys)
	if err != nil || len(values) != 3 || values[0] != "one" || values[1] != "" ||
		values[2] != "three" || !found[0] || found[1] || !found[2] {
		t.Fatalf("fallback values=%v found=%v error=%v", values, found, err)
	}
	presence, err := keyspace.Presence(t.Context(), tx, keys)
	if err != nil || len(presence) != 3 || !presence[0] || presence[1] || !presence[2] {
		t.Fatalf("fallback presence=%v error=%v", presence, err)
	}
	direct := &directPresenceBucket{scriptedBucket: bucket}
	presence, err = keyspace.Presence(
		t.Context(),
		&Txn{etx: presenceTxn{bucket: direct}},
		keys,
	)
	if err != nil || direct.checks != len(keys) || !presence[0] || presence[1] || !presence[2] {
		t.Fatalf("direct presence=%v checks=%d error=%v", presence, direct.checks, err)
	}
}

func TestKeyspaceSetReadRejectsBatchFailuresAndShapes(t *testing.T) {
	failure := errors.New("batch failed")
	keyspace := &Keyspace[string]{name: "data", codec: internalStringCodec{}}
	keys := []Key{Key("one"), Key("two")}
	cases := []struct {
		name   string
		bucket *scriptedSetReadBucket
		read   func(*Keyspace[string], *Txn, []Key) error
	}{
		{
			name: "value failure",
			bucket: &scriptedSetReadBucket{
				scriptedBucket: &scriptedBucket{},
				valueFailure:   failure,
			},
			read: func(keyspace *Keyspace[string], tx *Txn, keys []Key) error {
				_, _, err := keyspace.Values(t.Context(), tx, keys)

				return err
			},
		},
		{
			name: "value shape",
			bucket: &scriptedSetReadBucket{
				scriptedBucket: &scriptedBucket{},
				values:         [][]byte{[]byte("one")},
				valuePresence:  []bool{true, false},
			},
			read: func(keyspace *Keyspace[string], tx *Txn, keys []Key) error {
				_, _, err := keyspace.Values(t.Context(), tx, keys)

				return err
			},
		},
		{
			name: "presence failure",
			bucket: &scriptedSetReadBucket{
				scriptedBucket:  &scriptedBucket{},
				presenceFailure: failure,
			},
			read: func(keyspace *Keyspace[string], tx *Txn, keys []Key) error {
				_, err := keyspace.Presence(t.Context(), tx, keys)

				return err
			},
		},
		{
			name: "presence shape",
			bucket: &scriptedSetReadBucket{
				scriptedBucket: &scriptedBucket{},
				presence:       []bool{true},
			},
			read: func(keyspace *Keyspace[string], tx *Txn, keys []Key) error {
				_, err := keyspace.Presence(t.Context(), tx, keys)

				return err
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := test.read(
				keyspace,
				&Txn{etx: presenceTxn{bucket: test.bucket}},
				keys,
			)
			if err == nil {
				t.Fatal("invalid batch accepted")
			}
			if strings.Contains(test.name, "failure") && !errors.Is(err, failure) {
				t.Fatalf("batch error=%v, want=%v", err, failure)
			}
		})
	}
}

func TestKeyspaceSetReadRejectsDecodeAndFallbackCancellation(t *testing.T) {
	decodeFailure := errors.New("decode failed")
	bucket := &scriptedSetReadBucket{
		scriptedBucket: &scriptedBucket{},
		values:         [][]byte{[]byte("invalid")},
		valuePresence:  []bool{true},
	}
	keyspace := &Keyspace[string]{name: "data", codec: corruptValueCodec{failure: decodeFailure}}
	if _, _, err := keyspace.Values(
		t.Context(),
		&Txn{etx: presenceTxn{bucket: bucket}},
		[]Key{Key("key")},
	); !errors.Is(err, ErrCorruptValue) || !errors.Is(err, decodeFailure) {
		t.Fatalf("decode error=%v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	fallback := &scriptedBucket{values: map[string][]byte{"key": []byte("value")}}
	tx := &Txn{etx: presenceTxn{bucket: fallback}}
	plain := &Keyspace[string]{name: "data", codec: internalStringCodec{}}
	if _, _, err := plain.Values(
		canceled,
		tx,
		[]Key{Key("key")},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("value cancellation=%v", err)
	}
	if _, err := plain.Presence(
		canceled,
		tx,
		[]Key{Key("key")},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("presence cancellation=%v", err)
	}
}
