package frontiercheckpoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

var (
	ErrClosed              = errors.New("frontier checkpoint is closed")
	ErrCorruptCheckpoint   = errors.New("frontier checkpoint is corrupt")
	ErrFutureSchema        = errors.New("frontier checkpoint schema is newer than supported")
	ErrInvalidPath         = errors.New("frontier checkpoint path is empty")
	ErrInvalidProvenance   = errors.New("frontier checkpoint provenance is empty")
	ErrInvalidIdentity     = errors.New("frontier checkpoint order identity is empty")
	ErrInvalidHostState    = errors.New("frontier checkpoint host state is invalid")
	ErrInvalidPage         = errors.New("frontier checkpoint page is invalid")
	ErrInvalidSeedBatch    = errors.New("frontier checkpoint seed batch is invalid")
	ErrSeedManifestMissing = errors.New("frontier checkpoint seed manifest is missing")
	ErrInvalidControl      = errors.New("frontier checkpoint control update is empty")
	ErrInvalidWorkerPrefix = errors.New("frontier checkpoint worker prefix is empty")
	ErrProvenanceCollision = errors.New("frontier checkpoint provenance identifies another order")
	ErrRunCompleted        = errors.New("frontier checkpoint run is completed")
	ErrRunDeleting         = errors.New("frontier checkpoint run is being deleted")
	ErrRunNotFound         = errors.New("frontier checkpoint run was not found")
)

type RunStatus uint8

const (
	RunMissing RunStatus = iota
	RunActive
	RunCompleted
)

type Page struct {
	URL              string
	Host             string
	Depth            int
	ProfileHandle    string
	ObservationID    string
	ObservedAt       time.Time
	SourceModifiedAt time.Time
	RedirectURL      string
	RedirectHost     string
	RedirectHostBump bool
	Index            bool
}

type Redirect struct {
	SourceURL     string
	FinalURL      string
	FinalHost     string
	IncrementHost bool
}

type Counters struct {
	Pages   uint64
	Pending uint64
}

type HostState struct {
	Pages      uint64
	Failures   uint8
	Retired    bool
	Generation uint64
}

type HostProgress struct {
	Generation   uint64
	Failures     uint8
	Retired      bool
	Pace         crawlpace.HostState
	PaceCapacity int
}

type RunControl struct {
	Paused         bool
	Cancelled      bool
	PagesPerMinute *uint32
}

type ControlUpdate struct {
	Paused         *bool
	Cancelled      bool
	PagesPerMinute *uint32
}

type RunState struct {
	Status       RunStatus
	Pages        uint64
	Pending      uint64
	Failed       bool
	Seeding      bool
	SeedManifest bool
	Control      RunControl
	Tally        yagocrawlcontract.CrawlRunTally
}

type SeedDecision struct {
	Page  Page
	Admit bool
}

type SeedBatch struct {
	Cursor    uint64
	Decisions []SeedDecision
}

type SeedBatchResult struct {
	Admitted   int
	Duplicates uint64
}

type PageCompletion struct {
	Tally        yagocrawlcontract.CrawlRunTally
	HostProgress *PageHostProgress
}

type PageHostProgress struct {
	Host        string
	Progress    HostProgress
	DroppedURLs []string
}

type Snapshot struct {
	Visited              map[string]struct{}
	Counters             Counters
	BudgetDiscardedPages uint64
	HostStates           map[string]HostState
	Outstanding          []Page
	RecoveryBounded      bool
	RecoveryCursor       uint64
	RecoveryUpper        uint64
	RecoveryComplete     bool
	OrderIdentity        []byte
	Priority             yagocrawlcontract.CrawlOrderPriority
	Failed               bool
	Seeding              bool
	Completed            bool
	Control              RunControl
	Tally                yagocrawlcontract.CrawlRunTally
	SeedManifest         bool
	SeedPages            []Page
	SeedCursor           uint64
	SeedLength           uint64
}

type RecoveryPageBatch struct {
	Pages        []Page
	HostStates   map[string]HostState
	Cursor       uint64
	Complete     bool
	RetiredPages uint64
}

type AdmissionBatchState struct {
	Visited    []bool
	HostStates map[string]HostState
}

type FrontierCheckpoint struct {
	mutex                                  sync.RWMutex
	database                               *bolt.DB
	terminalSettlementReconciliationMutex  sync.Mutex
	terminalSettlementReconciliationCursor []byte
}

func (checkpoint *FrontierCheckpoint) readTransaction(
	ctx context.Context,
	read func(*bolt.Tx) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("read frontier checkpoint: %w", err)
	}
	checkpoint.mutex.RLock()
	defer checkpoint.mutex.RUnlock()
	if checkpoint.database == nil {
		return ErrClosed
	}
	return wrapDatabaseError("read frontier checkpoint", checkpoint.database.View(read))
}

func (checkpoint *FrontierCheckpoint) writeTransaction(
	ctx context.Context,
	write func(*bolt.Tx) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write frontier checkpoint: %w", err)
	}
	checkpoint.mutex.RLock()
	defer checkpoint.mutex.RUnlock()
	if checkpoint.database == nil {
		return ErrClosed
	}
	return wrapDatabaseError("write frontier checkpoint", checkpoint.database.Batch(write))
}

func wrapDatabaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
