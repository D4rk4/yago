// Package rwi owns RWI posting intake, storage, search, and eviction. It is the
// only writer of postings: callers read through PostingIndex, hand postings in
// through PostingReceiver, and drop them through PostingPurger, while projections
// follow arrivals and departures through PostingObserver. Every port speaks the
// yagomodel vocabulary and lends cross-module work a shared transaction, so the
// schema never leaks.
package rwi

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagoproto"
)

type PostingObserver interface {
	PostingStored(tx *vault.Txn, word, url yagomodel.Hash) error
	PostingPurged(tx *vault.Txn, word, url yagomodel.Hash) error
}

type PostingPurger interface {
	PurgePosting(tx *vault.Txn, word, url yagomodel.Hash) (bool, error)
}

type PostingIndex interface {
	RWICount(ctx context.Context) (int, error)
	RWIURLCount(ctx context.Context, word yagomodel.Hash) (int, error)
	ScanWord(
		ctx context.Context,
		word yagomodel.Hash,
		visit func(yagomodel.RWIPosting) (bool, error),
	) error
}

type PostingReceiver interface {
	Receive(ctx context.Context, entries []yagomodel.RWIPosting) (Receipt, error)
}

type Receipt struct {
	Busy       bool
	Pause      int
	UnknownURL []yagomodel.Hash
}

type Config struct {
	BatchCap          int
	PauseMilliseconds int
	// AcceptRemoteIndex mirrors the advertised accept-remote-index capability:
	// with it off, inbound transfers answer not_granted the way YaCy's
	// transferRWI does when allowReceiveIndex is disabled.
	AcceptRemoteIndex bool
}

func Open(
	vault *vault.Vault,
	urls urlmeta.URLDirectory,
	cfg Config,
	observers ...PostingObserver,
) (PostingIndex, PostingReceiver, PostingPurger, error) {
	postings, err := registerPostings(vault)
	if err != nil {
		return nil, nil, nil, err
	}
	outboundSelected, err := registerOutboundSelectedPostings(vault)
	if err != nil {
		return nil, nil, nil, err
	}

	watched := postingObservers(observers)
	directory := postingDirectory{
		vault:            vault,
		postings:         postings,
		outboundSelected: outboundSelected,
		observers:        watched,
	}
	intake := postingIntake{
		vault:             vault,
		postings:          postings,
		observers:         watched,
		urls:              urls,
		pauseMilliseconds: cfg.PauseMilliseconds,
	}

	return directory, intake, directory, nil
}

func MountTransferRWI(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	receiver PostingReceiver,
	gate *httpguard.IntakeGate,
	cfg Config,
) {
	httpguard.Mount(
		router,
		yagoproto.PathTransferRWI,
		yagoproto.TransferRWIEndpointMethods,
		yagoproto.ParseTransferRWIRequest,
		transferRWIEndpoint{
			identity:          identity,
			intake:            receiver,
			gate:              gate,
			batchCap:          cfg.BatchCap,
			pauseMilliseconds: cfg.PauseMilliseconds,
			accept:            cfg.AcceptRemoteIndex,
		}.Serve,
	)
}
