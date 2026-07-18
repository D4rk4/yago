package frontiercheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (checkpoint *FrontierCheckpoint) FinishSeeding(
	ctx context.Context,
	provenance []byte,
	tally yagocrawlcontract.CrawlRunTally,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	cleanup, err := checkpoint.prepareSeedingFinish(ctx, provenance, prefix, tally)
	if err != nil || !cleanup {
		return err
	}

	return checkpoint.deleteConsumedSeedManifest(ctx, provenance, prefix)
}

func (checkpoint *FrontierCheckpoint) CompletePage(
	ctx context.Context,
	provenance []byte,
	pageURL string,
	completion PageCompletion,
) error {
	prefix, err := provenancePrefix(provenance)
	if err != nil {
		return err
	}
	if strings.TrimSpace(pageURL) == "" {
		return ErrInvalidPage
	}
	if err := validatePageHostProgress(completion.HostProgress); err != nil {
		return err
	}
	present, err := checkpoint.outstandingPagePresent(ctx, provenance, prefix, pageURL)
	if err != nil || !present {
		return err
	}
	if completion.HostProgress != nil {
		if err := checkpoint.validateHostPages(
			ctx,
			provenance,
			prefix,
			completion.HostProgress.Host,
			completion.HostProgress.DroppedURLs,
		); err != nil {
			return err
		}
	}
	hostProgressCurrent := false
	err = checkpoint.writeTransaction(ctx, func(transaction *bolt.Tx) error {
		var completionErr error
		hostProgressCurrent, completionErr = completeOutstandingPage(
			transaction,
			provenance,
			prefix,
			pageURL,
			completion,
		)

		return completionErr
	})
	if err != nil || completion.HostProgress == nil || !hostProgressCurrent {
		return err
	}
	hostProgress := completion.HostProgress

	return checkpoint.removeHostPagesInChunks(
		ctx,
		hostPageRemoval{
			provenance: provenance,
			prefix:     prefix,
			host:       hostProgress.Host,
			generation: hostProgress.Progress.Generation,
			pageURLs:   hostProgress.DroppedURLs,
		},
	)
}

func completeOutstandingPage(
	transaction *bolt.Tx,
	provenance []byte,
	prefix []byte,
	pageURL string,
	completion PageCompletion,
) (bool, error) {
	record, err := requiredRunRecord(transaction, provenance)
	if err != nil {
		return false, err
	}
	buckets, err := loadCheckpointBuckets(transaction)
	if err != nil {
		return false, err
	}
	_, found, err := findOutstandingPage(buckets, prefix, pageURL)
	if err != nil || !found {
		return false, err
	}
	hostProgressCurrent, err := applyPageHostProgress(transaction, buckets, prefix, completion)
	if err != nil {
		return false, err
	}
	removed, err := removeOutstandingPage(buckets, prefix, pageURL, "")
	if err != nil || !removed {
		return false, err
	}
	if record.Pending == 0 {
		return false, fmt.Errorf("%w: outstanding page exceeds pending total", ErrCorruptCheckpoint)
	}
	record.Pending--
	record.Tally, err = addRunTally(record.Tally, completion.Tally)
	if err != nil {
		return false, err
	}
	markCompletion(&record, buckets.pages, prefix)

	return hostProgressCurrent, writeRunRecord(transaction, provenance, record)
}

func applyPageHostProgress(
	transaction *bolt.Tx,
	buckets checkpointBuckets,
	prefix []byte,
	completion PageCompletion,
) (bool, error) {
	if completion.HostProgress == nil {
		return false, nil
	}
	hostProgress := completion.HostProgress

	return applyHostProgress(
		transaction,
		buckets,
		prefix,
		hostProgress.Host,
		hostProgress.Progress,
	)
}

func validatePageHostProgress(hostProgress *PageHostProgress) error {
	if hostProgress == nil {
		return nil
	}
	if strings.TrimSpace(hostProgress.Host) == "" {
		return ErrInvalidPage
	}
	if err := validateHostPaceProgress(hostProgress.Progress); err != nil {
		return err
	}
	for _, pageURL := range hostProgress.DroppedURLs {
		if strings.TrimSpace(pageURL) == "" {
			return ErrInvalidPage
		}
	}
	if len(hostProgress.DroppedURLs) > 0 && !hostProgress.Progress.Retired {
		return ErrInvalidHostState
	}

	return nil
}

func (checkpoint *FrontierCheckpoint) outstandingPagePresent(
	ctx context.Context,
	provenance []byte,
	prefix []byte,
	pageURL string,
) (bool, error) {
	present := false
	err := checkpoint.readTransaction(ctx, func(transaction *bolt.Tx) error {
		if _, err := requiredRunRecord(transaction, provenance); err != nil {
			return err
		}
		buckets, err := loadCheckpointBuckets(transaction)
		if err != nil {
			return err
		}
		_, present, err = findOutstandingPage(buckets, prefix, pageURL)

		return err
	})

	return present, err
}

func removeOutstandingPage(
	buckets checkpointBuckets,
	prefix []byte,
	pageURL string,
	requiredHost string,
) (bool, error) {
	positionKey := childRowKey(prefix, pageURL)
	row, found, err := findOutstandingPage(buckets, prefix, pageURL)
	if err != nil || !found {
		return false, err
	}
	if requiredHost != "" && row.page.Host != requiredHost {
		return false, fmt.Errorf("%w: outstanding page identity mismatch", ErrCorruptCheckpoint)
	}
	return true, errors.Join(
		deleteRow(buckets.pages, row.key, "outstanding page"),
		deleteRow(buckets.pagePositions, positionKey, "page position"),
	)
}

func markCompletion(record *runRecord, pages *bolt.Bucket, prefix []byte) {
	key, _ := pages.Cursor().Seek(prefix)
	pagesRemain := key != nil && bytes.HasPrefix(key, prefix)
	record.Completed = !record.Seeding && record.Pending == 0 && !pagesRemain
}
