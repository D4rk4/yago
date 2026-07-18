package frontiercheckpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	databaseLockTimeout  = 250 * time.Millisecond
	checkpointBatchDelay = 2 * time.Millisecond
	checkpointBatchSize  = 256
)

func privateDirectoryMode() os.FileMode {
	return 0o700
}

func Open(path string) (*FrontierCheckpoint, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("open frontier checkpoint: %w", ErrInvalidPath)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, privateDirectoryMode()); err != nil {
		return nil, fmt.Errorf("create frontier checkpoint directory: %w", err)
	}
	if err := os.Chmod(directory, privateDirectoryMode()); err != nil {
		return nil, fmt.Errorf("secure frontier checkpoint directory: %w", err)
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: databaseLockTimeout})
	if err != nil {
		return nil, fmt.Errorf("open frontier checkpoint database: %w", err)
	}
	database.NoSync = false
	database.MaxBatchDelay = checkpointBatchDelay
	database.MaxBatchSize = checkpointBatchSize
	checkpoint := &FrontierCheckpoint{database: database}
	if err := checkpoint.initialize(path); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	if err := checkpoint.resumeDeletions(context.Background()); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	if err := checkpoint.resumeSeedManifestTransitions(context.Background()); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	if err := checkpoint.resumeCancelledRuns(context.Background()); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	if err := checkpoint.resumeRetiredHostTransitions(context.Background()); err != nil {
		return nil, errors.Join(err, database.Close())
	}
	return checkpoint, nil
}

func (checkpoint *FrontierCheckpoint) initialize(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure frontier checkpoint database: %w", err)
	}
	return checkpoint.writeTransaction(context.Background(), initializeSchema)
}

func (checkpoint *FrontierCheckpoint) Close() error {
	checkpoint.mutex.Lock()
	defer checkpoint.mutex.Unlock()
	if checkpoint.database == nil {
		return nil
	}
	database := checkpoint.database
	checkpoint.database = nil
	return wrapDatabaseError("close frontier checkpoint", database.Close())
}

func (checkpoint *FrontierCheckpoint) WorkerID(prefix string) (string, error) {
	if strings.TrimSpace(prefix) == "" ||
		!yagocrawlcontract.ValidCrawlerWorkerIdentity(prefix+"-"+uuid.Nil.String()) {
		return "", ErrInvalidWorkerPrefix
	}
	candidateSuffix := uuid.NewString()
	var identity string
	err := checkpoint.writeTransaction(context.Background(), func(transaction *bolt.Tx) error {
		identity = ""
		metadata, err := schemaBucket(transaction, metadataBucket)
		if err != nil {
			return err
		}
		identity = string(metadata.Get(workerIdentityKey))
		if identity != "" {
			if !yagocrawlcontract.ValidCrawlerWorkerIdentity(identity) {
				return fmt.Errorf("%w: invalid worker identity", ErrCorruptCheckpoint)
			}
			return nil
		}
		persistedSuffix := string(metadata.Get(workerSuffixKey))
		var suffixWriteErr error
		if persistedSuffix == "" {
			persistedSuffix = candidateSuffix
			suffixWriteErr = putRow(
				metadata,
				workerSuffixKey,
				[]byte(persistedSuffix),
				"worker suffix",
			)
		}
		identity = prefix + "-" + persistedSuffix
		if !yagocrawlcontract.ValidCrawlerWorkerIdentity(identity) {
			return fmt.Errorf("%w: invalid worker suffix", ErrCorruptCheckpoint)
		}
		return errors.Join(
			suffixWriteErr,
			putRow(metadata, workerIdentityKey, []byte(identity), "worker identity"),
		)
	})
	if err != nil {
		return "", err
	}
	return identity, nil
}
