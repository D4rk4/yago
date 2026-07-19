package frontiercheckpoint

import (
	"errors"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

type frontierStateStartupRunner func(string, time.Duration, func() error) error

type frontierStateDatabaseOpener func(
	string,
	os.FileMode,
	*bolt.Options,
) (*bolt.DB, error)

func openFrontierStateDatabase(
	path string,
	maximumBytes uint64,
	maintenance StateMaintenanceAdmission,
) (*bolt.DB, error) {
	return openFrontierStateDatabaseWithStartup(
		path,
		maximumBytes,
		maintenance,
		runWithFrontierStateStartupLease,
		bolt.Open,
	)
}

func openFrontierStateDatabaseWithStartup(
	path string,
	maximumBytes uint64,
	maintenance StateMaintenanceAdmission,
	runStartup frontierStateStartupRunner,
	openDatabase frontierStateDatabaseOpener,
) (*bolt.DB, error) {
	var database *bolt.DB
	err := runStartup(path, databaseLockTimeout, func() error {
		compaction, compactErr := compactFrontierState(path, maximumBytes, maintenance)
		reportFrontierStateCompaction(path, maximumBytes, compaction, compactErr)
		var openErr error
		database, openErr = openDatabase(
			path,
			frontierStateFileMode,
			&bolt.Options{Timeout: databaseLockTimeout},
		)
		if openErr != nil {
			return fmt.Errorf("open frontier checkpoint database: %w", openErr)
		}

		return nil
	})
	if err == nil {
		return database, nil
	}
	if database != nil {
		err = errors.Join(err, database.Close())
	}

	return nil, err
}
