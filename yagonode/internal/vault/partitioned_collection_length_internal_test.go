package vault

import (
	"errors"
	"testing"
)

type partitionedCollectionLengthTxn struct {
	scriptedTxn
	additions  int
	removals   int
	changesErr error
	recordErr  error
	recorded   []string
}

func (t *partitionedCollectionLengthTxn) RecordCollectionAddition(
	collection Name,
	record Key,
) error {
	t.recorded = append(t.recorded, "+"+string(collection)+":"+string(record))

	return t.recordErr
}

func (t *partitionedCollectionLengthTxn) RecordCollectionRemoval(
	collection Name,
	record Key,
) error {
	t.recorded = append(t.recorded, "-"+string(collection)+":"+string(record))

	return t.recordErr
}

func (t *partitionedCollectionLengthTxn) CollectionLengthChanges(Name) (int, int, error) {
	return t.additions, t.removals, t.changesErr
}

func TestPartitionedCollectionLengthCombinesLegacyAndChanges(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	for _, test := range []struct {
		name      string
		base      int
		additions int
		removals  int
		changes   error
		want      int
		wantError bool
	}{
		{name: "combined", base: 3, additions: 2, removals: 1, want: 4},
		{name: "zero floor", base: 1, removals: 1, want: 0},
		{name: "change read failure", changes: errors.New("read failed"), wantError: true},
		{name: "overflow", base: maximum, additions: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var lengths scriptedBucket
			lengths.values = map[string][]byte{}
			if err := putLength(&lengths, Key("data"), test.base); err != nil {
				t.Fatalf("seed base: %v", err)
			}
			partitioned := &partitionedCollectionLengthTxn{
				scriptedTxn: scriptedTxn{buckets: map[Name]*scriptedBucket{
					lengthBucket: &lengths,
				}},
				additions:  test.additions,
				removals:   test.removals,
				changesErr: test.changes,
			}
			got, err := readLength(&Txn{etx: partitioned}, "data")
			if (err != nil) != test.wantError {
				t.Fatalf("read length error = %v, wantError %v", err, test.wantError)
			}
			if err == nil && got != test.want {
				t.Fatalf("length = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPartitionedCollectionLengthRejectsCorruptLegacyBase(t *testing.T) {
	partitioned := &partitionedCollectionLengthTxn{scriptedTxn: scriptedTxn{
		buckets: map[Name]*scriptedBucket{
			lengthBucket: {values: map[string][]byte{"data": []byte("bad")}},
		},
	}}
	if _, err := readLength(&Txn{etx: partitioned}, "data"); err == nil {
		t.Fatal("corrupt legacy base was accepted")
	}
}

func TestPartitionedCollectionMutationsRecordShardLocalChanges(t *testing.T) {
	data := &scriptedBucket{values: map[string][]byte{}}
	lengths := &scriptedBucket{values: map[string][]byte{}}
	partitioned := &partitionedCollectionLengthTxn{scriptedTxn: scriptedTxn{
		writable: true,
		buckets: map[Name]*scriptedBucket{
			"data":       data,
			lengthBucket: lengths,
		},
	}}
	collection := &Collection[string]{name: "data", codec: internalStringCodec{}}
	txn := &Txn{etx: partitioned}
	if err := collection.Put(txn, Key("key"), "value"); err != nil {
		t.Fatalf("put: %v", err)
	}
	deleted, err := collection.Delete(txn, Key("key"))
	if err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	if len(partitioned.recorded) != 2 ||
		partitioned.recorded[0] != "+data:key" ||
		partitioned.recorded[1] != "-data:key" {
		t.Fatalf("recorded changes = %v", partitioned.recorded)
	}
	if len(lengths.values) != 0 {
		t.Fatalf("legacy counter changed: %v", lengths.values)
	}
}

func TestPartitionedCollectionMutationSurfacesChangeFailure(t *testing.T) {
	sentinel := errors.New("record failed")
	partitioned := &partitionedCollectionLengthTxn{
		scriptedTxn: scriptedTxn{
			writable: true,
			buckets: map[Name]*scriptedBucket{
				"data":       {values: map[string][]byte{}},
				lengthBucket: {values: map[string][]byte{}},
			},
		},
		recordErr: sentinel,
	}
	collection := &Collection[string]{name: "data", codec: internalStringCodec{}}
	err := collection.Put(&Txn{etx: partitioned}, Key("key"), "value")
	if !errors.Is(err, sentinel) {
		t.Fatalf("put error = %v, want %v", err, sentinel)
	}
}

func TestPartitionedCollectionRemovalSurfacesChangeFailure(t *testing.T) {
	sentinel := errors.New("record failed")
	partitioned := &partitionedCollectionLengthTxn{
		scriptedTxn: scriptedTxn{
			writable: true,
			buckets: map[Name]*scriptedBucket{
				"data":       {values: map[string][]byte{"key": []byte("value")}},
				lengthBucket: {values: map[string][]byte{}},
			},
		},
		recordErr: sentinel,
	}
	collection := &Collection[string]{name: "data", codec: internalStringCodec{}}
	_, err := collection.Delete(&Txn{etx: partitioned}, Key("key"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("delete error = %v, want %v", err, sentinel)
	}
}
