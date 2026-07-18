package frontiercheckpoint

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	initialSchemaVersion        uint32 = 1
	hostPaceSchemaVersion       uint32 = 2
	seedManifestSchemaVersion   uint32 = 3
	terminalOutboxSchemaVersion uint32 = 4
	currentSchemaVersion        uint32 = 5
)

var (
	metadataBucket       = []byte("metadata")
	runsBucket           = []byte("runs")
	visitedBucket        = []byte("visited")
	pagesBucket          = []byte("pages")
	pagePositionsBucket  = []byte("page_positions")
	hostsBucket          = []byte("hosts")
	hostPacesBucket      = []byte("host_paces")
	hostPaceOrderBucket  = []byte("host_pace_order")
	seedManifestBucket   = []byte("seed_manifest")
	terminalOutboxBucket = []byte("terminal_outbox")
	schemaVersionKey     = []byte("schema_version")
	workerIdentityKey    = []byte("worker_identity")
	workerSuffixKey      = []byte("worker_suffix")
	hostPaceSequenceKey  = []byte("host_pace_sequence")
	hostPaceTotalKey     = []byte("host_pace_total")
	visitedMarker        = []byte{1}
	initialSchemaBuckets = [][]byte{
		metadataBucket,
		runsBucket,
		visitedBucket,
		pagesBucket,
		pagePositionsBucket,
		hostsBucket,
	}
	allSchemaBuckets = append(
		append([][]byte(nil), initialSchemaBuckets...),
		hostPacesBucket,
		hostPaceOrderBucket,
		seedManifestBucket,
		terminalOutboxBucket,
	)
)

type runRecord struct {
	OrderIdentity          []byte                               `json:"order_identity"`
	Priority               yagocrawlcontract.CrawlOrderPriority `json:"priority"`
	NextSequence           uint64                               `json:"next_sequence"`
	Pages                  uint64                               `json:"pages"`
	Pending                uint64                               `json:"pending"`
	Failed                 bool                                 `json:"failed"`
	Seeding                bool                                 `json:"seeding"`
	Completed              bool                                 `json:"completed"`
	Deleting               bool                                 `json:"deleting"`
	Paused                 bool                                 `json:"paused"`
	Cancelled              bool                                 `json:"cancelled"`
	PagesPerMinute         *uint32                              `json:"pages_per_minute,omitempty"`
	Tally                  yagocrawlcontract.CrawlRunTally      `json:"tally"`
	SeedManifest           bool                                 `json:"seed_manifest"`
	SeedLength             uint64                               `json:"seed_length"`
	SeedCursor             uint64                               `json:"seed_cursor"`
	SeedManifestPublishing bool                                 `json:"seed_manifest_publishing"`
	SeedManifestIdentity   []byte                               `json:"seed_manifest_identity,omitempty"`
	SeedManifestDeleting   bool                                 `json:"seed_manifest_deleting"`
	SeedManifestConsumed   bool                                 `json:"seed_manifest_consumed"`
}

type hostRecord struct {
	Pages             uint64 `json:"pages"`
	Failures          uint8  `json:"failures"`
	Retired           bool   `json:"retired"`
	Generation        uint64 `json:"generation"`
	RetirementCursor  uint64 `json:"retirement_cursor,omitempty"`
	RetirementScanned bool   `json:"retirement_scanned,omitempty"`
}

func initializeSchema(transaction *bolt.Tx) error {
	metadata := transaction.Bucket(metadataBucket)
	if metadata == nil {
		return createSchema(transaction)
	}
	versionBytes := metadata.Get(schemaVersionKey)
	if len(versionBytes) != 4 {
		return fmt.Errorf("%w: invalid schema version", ErrCorruptCheckpoint)
	}
	version := binary.BigEndian.Uint32(versionBytes)
	if version > currentSchemaVersion {
		return fmt.Errorf("%w: version %d", ErrFutureSchema, version)
	}
	if version == initialSchemaVersion {
		if err := migrateInitialSchema(transaction); err != nil {
			return err
		}
		version = hostPaceSchemaVersion
	}
	if version == hostPaceSchemaVersion {
		if err := migrateHostPaceSchema(transaction); err != nil {
			return err
		}
		version = seedManifestSchemaVersion
	}
	if version == seedManifestSchemaVersion {
		if err := migrateSeedManifestSchema(transaction); err != nil {
			return err
		}
		version = terminalOutboxSchemaVersion
	}
	if version == terminalOutboxSchemaVersion {
		return migrateTerminalOutboxSchema(transaction)
	}
	if version != currentSchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %d", ErrCorruptCheckpoint, version)
	}
	return validateSchemaBuckets(transaction, allSchemaBuckets)
}

func validateSchemaBuckets(transaction *bolt.Tx, names [][]byte) error {
	for _, name := range names {
		if transaction.Bucket(name) == nil {
			return fmt.Errorf("%w: missing bucket %q", ErrCorruptCheckpoint, name)
		}
	}
	return nil
}

func migrateInitialSchema(transaction *bolt.Tx) error {
	if err := validateSchemaBuckets(transaction, initialSchemaBuckets); err != nil {
		return err
	}
	for _, name := range [][]byte{hostPacesBucket, hostPaceOrderBucket} {
		if _, err := transaction.CreateBucket(name); err != nil {
			return fmt.Errorf("create frontier checkpoint bucket %q: %w", name, err)
		}
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, hostPaceSchemaVersion)
	return putRow(transaction.Bucket(metadataBucket), schemaVersionKey, version, "schema version")
}

func migrateHostPaceSchema(transaction *bolt.Tx) error {
	if err := validateSchemaBuckets(
		transaction,
		append(
			append([][]byte(nil), initialSchemaBuckets...),
			hostPacesBucket,
			hostPaceOrderBucket,
		),
	); err != nil {
		return err
	}
	if _, err := transaction.CreateBucket(seedManifestBucket); err != nil {
		return fmt.Errorf("create frontier checkpoint bucket %q: %w", seedManifestBucket, err)
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, seedManifestSchemaVersion)
	return putRow(transaction.Bucket(metadataBucket), schemaVersionKey, version, "schema version")
}

func migrateSeedManifestSchema(transaction *bolt.Tx) error {
	if err := validateSchemaBuckets(
		transaction,
		append(
			append([][]byte(nil), initialSchemaBuckets...),
			hostPacesBucket,
			hostPaceOrderBucket,
			seedManifestBucket,
		),
	); err != nil {
		return err
	}
	if _, err := transaction.CreateBucket(terminalOutboxBucket); err != nil {
		return fmt.Errorf("create frontier checkpoint bucket %q: %w", terminalOutboxBucket, err)
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, terminalOutboxSchemaVersion)
	return putRow(transaction.Bucket(metadataBucket), schemaVersionKey, version, "schema version")
}

func migrateTerminalOutboxSchema(transaction *bolt.Tx) error {
	if err := validateSchemaBuckets(transaction, allSchemaBuckets); err != nil {
		return err
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, currentSchemaVersion)
	return putRow(transaction.Bucket(metadataBucket), schemaVersionKey, version, "schema version")
}

func createSchema(transaction *bolt.Tx) error {
	cursor := transaction.Cursor()
	if key, _ := cursor.First(); key != nil {
		return fmt.Errorf("%w: schema metadata is missing", ErrCorruptCheckpoint)
	}
	for _, name := range allSchemaBuckets {
		if _, err := transaction.CreateBucket(name); err != nil {
			return fmt.Errorf("create frontier checkpoint bucket %q: %w", name, err)
		}
	}
	version := make([]byte, 4)
	binary.BigEndian.PutUint32(version, currentSchemaVersion)
	return putRow(transaction.Bucket(metadataBucket), schemaVersionKey, version, "schema version")
}

func schemaBucket(transaction *bolt.Tx, name []byte) (*bolt.Bucket, error) {
	bucket := transaction.Bucket(name)
	if bucket == nil {
		return nil, fmt.Errorf("%w: missing bucket %q", ErrCorruptCheckpoint, name)
	}
	return bucket, nil
}

func encodeRow(name string, value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode frontier checkpoint %s: %w", name, err)
	}
	return encoded, nil
}

func decodeRow(name string, encoded []byte, value any) error {
	if err := json.Unmarshal(encoded, value); err != nil {
		return fmt.Errorf("%w: decode %s: %w", ErrCorruptCheckpoint, name, err)
	}
	return nil
}

func putRow(bucket *bolt.Bucket, key, value []byte, name string) error {
	return wrapDatabaseError("write frontier checkpoint "+name, bucket.Put(key, value))
}

func deleteRow(bucket *bolt.Bucket, key []byte, name string) error {
	return wrapDatabaseError("delete frontier checkpoint "+name, bucket.Delete(key))
}

func provenancePrefix(provenance []byte) ([]byte, error) {
	if len(provenance) == 0 || len(provenance) > (bolt.MaxKeySize-2)/2 {
		return nil, ErrInvalidProvenance
	}
	prefix := make([]byte, 0, len(provenance)+2)
	for _, value := range provenance {
		if value == 0 {
			prefix = append(prefix, 0)
		}
		prefix = append(prefix, value)
	}
	prefix = append(prefix, 0, 1)
	return prefix, nil
}

func childRowKey(prefix []byte, child string) []byte {
	key := make([]byte, len(prefix)+len(child))
	copy(key, prefix)
	copy(key[len(prefix):], child)
	return key
}

func sequenceRowKey(prefix []byte, sequence uint64) []byte {
	key := make([]byte, len(prefix)+8)
	copy(key, prefix)
	binary.BigEndian.PutUint64(key[len(prefix):], sequence)
	return key
}

func sequenceValue(sequence uint64) []byte {
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, sequence)
	return value
}

func nextValue(value uint64) (uint64, error) {
	if value == math.MaxUint64 {
		return 0, fmt.Errorf("%w: counter overflow", ErrCorruptCheckpoint)
	}
	return value + 1, nil
}
