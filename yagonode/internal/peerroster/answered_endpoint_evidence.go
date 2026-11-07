package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peerAnsweredEndpointUnreadableMessage = "peer answered endpoint unreadable"

func (r *roster) AnsweredEndpoint(
	ctx context.Context,
	peer yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	var (
		answered peerwire.Endpoint
		found    bool
	)
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		key := r.key(peer)
		entry, known, err := r.getRosterEntry(tx, key)
		if err != nil {
			return err
		}
		if known {
			answered, found = r.answeredEndpointFor(ctx, tx, peer, entry)
		}

		return nil
	}); err != nil {
		return peerwire.Endpoint{}, false, fmt.Errorf("read answered endpoint: %w", err)
	}

	return answered, found, nil
}

func (r *roster) answeredEndpointFor(
	ctx context.Context,
	tx *vault.Txn,
	peer yagomodel.Hash,
	entry rosterEntry,
) (peerwire.Endpoint, bool) {
	record, recorded, err := r.answeredEndpoints.Get(tx, r.key(peer))
	if err != nil {
		slog.WarnContext(
			ctx,
			peerAnsweredEndpointUnreadableMessage,
			slog.String("peer", peer.String()),
		)

		return peerwire.Endpoint{}, false
	}
	if !recorded || !record.appliesTo(entry) {
		return peerwire.Endpoint{}, false
	}

	return record.endpoint, true
}

func (r *roster) ObserveAnsweredResponder(
	ctx context.Context,
	responder yagomodel.Seed,
	endpoint peerwire.Endpoint,
) {
	r.observeResponder(ctx, responder, yagomodel.Some(endpoint))
}

func (r *roster) ObserveAnsweredCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	endpoint peerwire.Endpoint,
) {
	r.observeCaller(ctx, caller, classification, yagomodel.Some(endpoint))
}

func (r *roster) storeAnsweredEndpoint(
	tx *vault.Txn,
	key vault.Key,
	entry rosterEntry,
	answered yagomodel.Optional[peerwire.Endpoint],
) error {
	record, changed := r.answeredEndpointChange(tx, key, entry, answered)
	if !changed {
		return nil
	}
	if err := r.answeredEndpoints.Put(tx, key, record); err != nil {
		return fmt.Errorf("store answered endpoint: %w", err)
	}

	return nil
}

func (r *roster) answeredEndpointChange(
	tx *vault.Txn,
	key vault.Key,
	entry rosterEntry,
	answered yagomodel.Optional[peerwire.Endpoint],
) (answeredEndpointRecord, bool) {
	endpoint, present := answered.Get()
	if !present {
		return answeredEndpointRecord{}, false
	}
	record, admitted := answeredEndpointRecordFor(entry, endpoint)
	if !admitted {
		return answeredEndpointRecord{}, false
	}
	stored, found, err := r.answeredEndpoints.Get(tx, key)

	return record, err != nil || !found || stored != record
}
