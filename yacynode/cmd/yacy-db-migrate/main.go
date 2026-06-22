// Command yacy-db-migrate upgrades a legacy YaCy node database to the vault bucket schema.
//
// It renames the legacy data buckets, rebuilds per-bucket length counters in the
// vault length bucket, and drops the obsolete counts bucket. The run is offline,
// idempotent, and atomic: a single bolt transaction either upgrades the whole
// database or leaves it untouched.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	bolt "go.etcd.io/bbolt"
)

const dbPathFlag = "db"

var errMissingDBPath = errors.New("database path is required")

func main() {
	path := flag.String(dbPathFlag, "", "path to the YaCy node database file")
	flag.Parse()

	if err := run(*path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, out io.Writer) error {
	if path == "" {
		return errMissingDBPath
	}

	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	migrated, migrateErr := migrate(db)
	if closeErr := db.Close(); closeErr != nil && migrateErr == nil {
		migrateErr = fmt.Errorf("close database: %w", closeErr)
	}
	if migrateErr != nil {
		return migrateErr
	}

	message := "database already on vault schema"
	if migrated {
		message = "database migrated to vault schema"
	}
	if _, err := fmt.Fprintln(out, message); err != nil {
		return fmt.Errorf("write result: %w", err)
	}

	return nil
}
