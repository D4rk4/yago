package remotecrawl

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	ReceiptAcceptedDelay                   = 10
	ReceiptRetryDelay                      = 3600
	ReceiptPolicyDelay                     = 9999
	remoteCrawlMetadataStoreFailureMessage = "remote crawl metadata store failed"
)

func (b *Broker) ProcessReceipt(
	ctx context.Context,
	req yagoproto.CrawlReceiptRequest,
) (yagoproto.CrawlReceiptResponse, error) {
	metadata, accepted := b.acceptReceiptMetadata(ctx, req)
	if !accepted {
		return yagoproto.CrawlReceiptResponse{Delay: ReceiptRetryDelay}, nil
	}
	canonical, policyErr := b.policy.Admit(ctx, metadata.url)

	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.config.Now().UTC()
	record, found, err := b.leasedRecord(ctx, req.Iam, metadata.hash.Hash(), now)
	if err != nil {
		return yagoproto.CrawlReceiptResponse{}, err
	}
	if !found {
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "lease_rejected", Peer: req.Iam,
			URLHash: metadata.hash.Hash(),
		}, true)

		return yagoproto.CrawlReceiptResponse{Delay: ReceiptRetryDelay}, nil
	}
	if policyErr != nil || canonical != record.URL || metadata.hash.String() != record.URLHash {
		if err := b.requeueRecord(ctx, record); err != nil {
			return yagoproto.CrawlReceiptResponse{}, err
		}
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "destination_rejected", Peer: req.Iam,
			URLHash: metadata.hash.Hash(),
		}, true)

		return yagoproto.CrawlReceiptResponse{Delay: ReceiptPolicyDelay}, nil
	}
	if req.Result != yagoproto.CrawlReceiptResultFill {
		if err := b.requeueRecord(ctx, record); err != nil {
			return yagoproto.CrawlReceiptResponse{}, err
		}
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "requeued", Peer: req.Iam,
			URLHash: metadata.hash.Hash(),
		}, false)

		return yagoproto.CrawlReceiptResponse{Delay: ReceiptRetryDelay}, nil
	}
	receipt, err := b.receiver.Receive(ctx, []yagomodel.URIMetadataRow{metadata.row})
	if err != nil || receipt.Busy || len(receipt.ErrorURL) != 0 {
		if requeueErr := b.requeueRecord(ctx, record); requeueErr != nil {
			return yagoproto.CrawlReceiptResponse{}, requeueErr
		}
		b.observe(ctx, Observation{
			Action: "receipt", Outcome: "store_requeued", Peer: req.Iam,
			URLHash: metadata.hash.Hash(),
		}, true)
		if err != nil {
			slog.WarnContext(
				ctx,
				remoteCrawlMetadataStoreFailureMessage,
				slog.String("peer", req.Iam.String()),
				slog.String("urlHash", metadata.hash.String()),
				slog.Any("error", err),
			)
		}

		return yagoproto.CrawlReceiptResponse{Delay: ReceiptRetryDelay}, nil
	}
	if err := b.deleteOrder(ctx, record); err != nil {
		return yagoproto.CrawlReceiptResponse{}, err
	}
	b.observe(ctx, Observation{
		Action: "receipt", Outcome: "accepted", Peer: req.Iam,
		URLHash: metadata.hash.Hash(), Count: 1,
	}, false)

	return yagoproto.CrawlReceiptResponse{Delay: ReceiptAcceptedDelay}, nil
}

func parseReceiptMetadata(
	ctx context.Context,
	raw string,
) (yagomodel.URIMetadataRow, string, yagomodel.URLHash, error) {
	if len(raw) == 0 || len(raw) > MaximumReceiptMetadataBytes {
		return yagomodel.URIMetadataRow{}, "", "", fmt.Errorf(
			"remote crawl metadata length is outside policy",
		)
	}
	decoded, err := yagomodel.DecodeWireFormWithLimit(
		ctx,
		raw,
		MaximumReceiptMetadataBytes,
	)
	if err != nil {
		return yagomodel.URIMetadataRow{}, "", "", fmt.Errorf(
			"decode remote crawl metadata: %w",
			err,
		)
	}
	row, err := yagomodel.ParseURIMetadataRow(decoded)
	if err != nil {
		return yagomodel.URIMetadataRow{}, "", "", fmt.Errorf(
			"parse remote crawl metadata: %w",
			err,
		)
	}
	urlValue, err := yagomodel.DecodeWireFormWithLimit(
		ctx,
		row.Properties[yagomodel.URLMetaURL],
		MaximumReceiptURLBytes,
	)
	if err != nil || urlValue == "" {
		return yagomodel.URIMetadataRow{}, "", "", fmt.Errorf("decode remote crawl metadata URL")
	}
	hash, _ := row.URLHash()
	computed, _ := yagomodel.HashURL(urlValue)
	if computed != hash {
		return yagomodel.URIMetadataRow{}, "", "", fmt.Errorf(
			"remote crawl metadata URL hash mismatch",
		)
	}

	return row, urlValue, hash, nil
}

func (b *Broker) leasedRecord(
	ctx context.Context,
	peer yagomodel.Hash,
	hash yagomodel.Hash,
	now time.Time,
) (queueRecord, bool, error) {
	var record queueRecord
	found := false
	err := b.storage.View(ctx, func(tx *vault.Txn) error {
		sequence, indexed, err := b.urlSequences.Get(tx, vault.Key(hash))
		if err != nil {
			return fmt.Errorf("read remote crawl URL sequence: %w", err)
		}
		if !indexed {
			return nil
		}
		record, found, err = b.orders.Get(tx, sequenceKey(sequence))
		if err != nil {
			return fmt.Errorf("read remote crawl lease: %w", err)
		}
		if !found || record.State != queueStateLeased || record.Peer != peer.String() {
			found = false

			return nil
		}

		return nil
	})
	if err != nil {
		return queueRecord{}, false, fmt.Errorf("verify remote crawl lease: %w", err)
	}
	if found && record.LeaseUntil <= now.UnixNano() {
		if err := b.requeueRecord(ctx, record); err != nil {
			return queueRecord{}, false, fmt.Errorf("expire remote crawl lease: %w", err)
		}

		return queueRecord{}, false, nil
	}

	return record, found, nil
}

func (b *Broker) requeueRecord(ctx context.Context, record queueRecord) error {
	if err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := b.releaseLease(tx, record); err != nil {
			return err
		}
		record.State = queueStatePending
		record.Peer = ""
		record.LeaseUntil = 0
		if err := b.orders.Put(tx, sequenceKey(record.Sequence), record); err != nil {
			return fmt.Errorf("store requeued remote crawl order: %w", err)
		}
		if err := b.pending.Put(
			tx,
			sequenceKey(record.Sequence),
			pendingRecord{Sequence: record.Sequence},
		); err != nil {
			return fmt.Errorf("index requeued remote crawl order: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("requeue remote crawl order: %w", err)
	}

	return nil
}
