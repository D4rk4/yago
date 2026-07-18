package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

func (checkpoint *FrontierCheckpoint) RecordRedirect(
	ctx context.Context,
	provenance []byte,
	redirect Redirect,
) (bool, error) {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return false, err
	}
	if err := validateRedirect(redirect); err != nil {
		return false, err
	}
	admitted := false
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		admitted = false
		record, err := requiredRunRecord(transaction, provenance)
		if err != nil {
			return err
		}
		if record.Completed {
			return ErrRunCompleted
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		row, found, err := findOutstandingPage(buckets, prefix, redirect.SourceURL)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: redirect source is not outstanding", ErrCorruptCheckpoint)
		}
		return reserveRedirect(buckets, prefix, row, redirect, &admitted)
	})
	return admitted, err
}

func validateRedirect(redirect Redirect) error {
	if strings.TrimSpace(redirect.SourceURL) == "" {
		return ErrInvalidPage
	}
	if redirect.FinalURL == "" {
		if redirect.FinalHost != "" || redirect.IncrementHost {
			return ErrInvalidPage
		}

		return nil
	}
	if strings.TrimSpace(redirect.FinalURL) == "" ||
		redirect.SourceURL == redirect.FinalURL ||
		strings.TrimSpace(redirect.FinalHost) == "" {
		return ErrInvalidPage
	}
	return nil
}

func reserveRedirect(
	buckets checkpointBuckets,
	prefix []byte,
	row outstandingPageRow,
	redirect Redirect,
	admitted *bool,
) error {
	if redirect.FinalURL != "" &&
		redirect.IncrementHost != (redirect.FinalHost != row.page.Host) {
		return ErrInvalidPage
	}
	if row.page.RedirectURL != "" {
		return updateRedirectReservation(buckets, prefix, row, redirect, admitted)
	}
	if redirect.FinalURL == "" {
		*admitted = true

		return nil
	}
	finalKey := childRowKey(prefix, redirect.FinalURL)
	visited := buckets.visited.Get(finalKey)
	if visited != nil {
		if !bytes.Equal(visited, visitedMarker) {
			return fmt.Errorf("%w: invalid redirect visited row", ErrCorruptCheckpoint)
		}
		return nil
	}
	row.page.RedirectURL = redirect.FinalURL
	row.page.RedirectHost = redirect.FinalHost
	row.page.RedirectHostBump = redirect.IncrementHost
	encoded, encodingError := encodeRow("redirected page", row.page)
	if err := errors.Join(
		encodingError,
		putRow(buckets.pages, row.key, encoded, "redirected page"),
		putRow(buckets.visited, finalKey, visitedMarker, "redirect target"),
	); err != nil {
		return err
	}
	if redirect.IncrementHost {
		if err := incrementHostPages(buckets.hosts, prefix, redirect.FinalHost); err != nil {
			return err
		}
	}
	*admitted = true
	return nil
}

func updateRedirectReservation(
	buckets checkpointBuckets,
	prefix []byte,
	row outstandingPageRow,
	redirect Redirect,
	admitted *bool,
) error {
	if err := validateExistingRedirect(buckets.visited, prefix, row.page); err != nil {
		return err
	}
	if row.page.RedirectURL == redirect.FinalURL {
		if row.page.RedirectHost != redirect.FinalHost ||
			row.page.RedirectHostBump != redirect.IncrementHost {
			return fmt.Errorf("%w: redirect ownership changed", ErrCorruptCheckpoint)
		}
		*admitted = true

		return nil
	}
	if err := releaseRedirectReservation(buckets, prefix, row.page); err != nil {
		return err
	}
	row.page.RedirectURL = ""
	row.page.RedirectHost = ""
	row.page.RedirectHostBump = false
	if redirect.FinalURL == "" {
		*admitted = true

		return writeRedirectedPage(buckets.pages, row)
	}
	visited := buckets.visited.Get(childRowKey(prefix, redirect.FinalURL))
	if visited != nil {
		if !bytes.Equal(visited, visitedMarker) {
			return fmt.Errorf("%w: invalid redirect visited row", ErrCorruptCheckpoint)
		}

		return writeRedirectedPage(buckets.pages, row)
	}
	row.page.RedirectURL = redirect.FinalURL
	row.page.RedirectHost = redirect.FinalHost
	row.page.RedirectHostBump = redirect.IncrementHost
	if err := writeRedirectedPage(buckets.pages, row); err != nil {
		return err
	}
	if err := putRow(
		buckets.visited,
		childRowKey(prefix, redirect.FinalURL),
		visitedMarker,
		"redirect target",
	); err != nil {
		return err
	}
	if redirect.IncrementHost {
		if err := incrementHostPages(buckets.hosts, prefix, redirect.FinalHost); err != nil {
			return err
		}
	}
	*admitted = true

	return nil
}

func validateExistingRedirect(visited *bolt.Bucket, prefix []byte, page Page) error {
	if page.RedirectHost == "" ||
		page.RedirectHostBump != (page.RedirectHost != page.Host) {
		return fmt.Errorf("%w: redirect ownership is invalid", ErrCorruptCheckpoint)
	}
	marker := visited.Get(childRowKey(prefix, page.RedirectURL))
	if !bytes.Equal(marker, visitedMarker) {
		return fmt.Errorf("%w: redirect target reservation is missing", ErrCorruptCheckpoint)
	}

	return nil
}

func releaseRedirectReservation(
	buckets checkpointBuckets,
	prefix []byte,
	page Page,
) error {
	if page.RedirectHostBump {
		if err := decrementHostPages(buckets.hosts, prefix, page.RedirectHost); err != nil {
			return err
		}
	}

	return deleteRow(
		buckets.visited,
		childRowKey(prefix, page.RedirectURL),
		"redirect target",
	)
}

func writeRedirectedPage(bucket *bolt.Bucket, row outstandingPageRow) error {
	encoded, err := encodeRow("redirected page", row.page)
	if err != nil {
		return err
	}

	return putRow(bucket, row.key, encoded, "redirected page")
}
