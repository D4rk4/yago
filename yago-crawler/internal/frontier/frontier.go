package frontier

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	pagesBucketName       = "pages"
	readyBucketName       = "ready"
	ingestBucketName      = "pending-ingest"
	ingestReadyBucketName = "pending-ingest-ready"
	ingestRunsBucketName  = "pending-ingest-runs"
	hostsBucketName       = "hosts"
	metadataBucketName    = "metadata"
	runsBucketName        = "runs"
	runHostsBucketName    = "run-hosts"
	frontierSchema        = uint64(1)
	frontierSchemaKey     = "schema-version"
	maximumReadyScan      = 4096
)

var ErrPendingLimit = errors.New("frontier pending page limit reached")
var ErrStateLimit = errors.New("frontier state byte limit reached")
var ErrHostPageLimit = errors.New("crawl run host page limit reached")
var ErrRunPageLimit = errors.New("crawl run page limit reached")

type State string

const (
	StateQueued    State = "queued"
	StateFetching  State = "fetching"
	StateFetched   State = "fetched"
	StateIndexed   State = "indexed"
	StateFailed    State = "failed"
	StateDeferred  State = "deferred"
	StateBlocked   State = "blocked"
	StateCancelled State = "cancelled"
)

type Config struct {
	MaximumPendingPages uint64
	MaximumStateBytes   uint64
	LeaseDuration       time.Duration
	HostDelay           time.Duration
	MaximumHostActive   int
}

type Frontier struct {
	database *bolt.DB
	config   Config
}

type Page struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	URL           string    `json:"url"`
	Host          string    `json:"host"`
	ReferrerURL   string    `json:"referrerUrl,omitempty"`
	Depth         int       `json:"depth"`
	State         State     `json:"state"`
	Attempts      uint32    `json:"attempts"`
	NotBefore     time.Time `json:"notBefore"`
	LeaseUntil    time.Time `json:"leaseUntil,omitempty"`
	CompletedAt   time.Time `json:"completedAt,omitempty"`
	Outcome       string    `json:"outcome,omitempty"`
	OutcomeReason string    `json:"outcomeReason,omitempty"`
}

type QueueDepth struct {
	Queued             uint64
	Fetching           uint64
	Fetched            uint64
	Indexed            uint64
	Failed             uint64
	Deferred           uint64
	Blocked            uint64
	Cancelled          uint64
	DocumentCandidates uint64
	RemovalMutations   uint64
	NotIndexable       uint64
	NodeRejected       uint64
	OutcomeCounts      []OutcomeCount
}

func Open(path string, config Config) (*Frontier, error) {
	if config.MaximumPendingPages == 0 && config.MaximumStateBytes == 0 || config.LeaseDuration <= 0 ||
		config.HostDelay < 0 || config.MaximumHostActive < 1 {
		return nil, errors.New("invalid frontier limits")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create frontier directory: %w", err)
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open frontier database: %w", err)
	}
	frontier := &Frontier{database: database, config: config}
	if err := frontier.initialize(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return frontier, nil
}

func (frontier *Frontier) Close() error {
	if err := frontier.database.Close(); err != nil {
		return fmt.Errorf("close frontier database: %w", err)
	}
	return nil
}

func (frontier *Frontier) initialize() error {
	err := frontier.database.Update(func(transaction *bolt.Tx) error {
		metadata, err := transaction.CreateBucketIfNotExists([]byte(metadataBucketName))
		if err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(pagesBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(readyBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(ingestBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(ingestReadyBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(ingestRunsBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(hostsBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(runsBucketName)); err != nil {
			return err
		}
		if _, err := transaction.CreateBucketIfNotExists([]byte(runHostsBucketName)); err != nil {
			return err
		}
		rawVersion := metadata.Get([]byte(frontierSchemaKey))
		if rawVersion != nil {
			if len(rawVersion) != 8 || binary.BigEndian.Uint64(rawVersion) != frontierSchema {
				return errors.New("unsupported frontier schema")
			}
			return nil
		}
		encoded := make([]byte, 8)
		binary.BigEndian.PutUint64(encoded, frontierSchema)
		return metadata.Put([]byte(frontierSchemaKey), encoded)
	})
	if err != nil {
		return fmt.Errorf("initialize frontier database: %w", err)
	}
	return nil
}

func loadPage(raw []byte) (Page, error) {
	var page Page
	if err := json.Unmarshal(raw, &page); err != nil {
		return Page{}, fmt.Errorf("decode frontier page: %w", err)
	}
	if page.ID == "" || page.RunID == "" || page.URL == "" || page.Host == "" ||
		!validState(page.State) {
		return Page{}, errors.New("invalid frontier page")
	}
	return page, nil
}

func savePage(bucket *bolt.Bucket, page Page) error {
	encoded, err := json.Marshal(page)
	if err != nil {
		return fmt.Errorf("encode frontier page: %w", err)
	}
	return bucket.Put([]byte(page.ID), encoded)
}

func validState(state State) bool {
	switch state {
	case StateQueued, StateFetching, StateFetched, StateIndexed, StateFailed, StateDeferred, StateBlocked, StateCancelled:
		return true
	default:
		return false
	}
}

func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
