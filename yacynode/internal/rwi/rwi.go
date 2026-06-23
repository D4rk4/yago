// Package rwi owns the transferRWI and search endpoints, RWI posting intake,
// posting storage with the referenced-URL set, and search. Its published port,
// PostingDirectory, is the only surface other modules import; it speaks the
// yacymodel vocabulary and lends cross-module purges a shared transaction, so the
// schema never leaks.
package rwi

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type PurgeResult struct {
	PostingsDeleted   int
	ReferencesDeleted int
}

type PostingDirectory interface {
	RWICount(ctx context.Context) (int, error)
	ReferencedURLCount(ctx context.Context) (int, error)
	PurgeReferences(tx *boltvault.Txn, urls []yacymodel.Hash) (PurgeResult, error)
}

type PostingScanner interface {
	ScanWord(
		ctx context.Context,
		word yacymodel.Hash,
		visit func(yacymodel.RWIPosting) (bool, error),
	) error
}

type PostingIndex interface {
	PostingDirectory
	PostingScanner
}

type PostingReceiver interface {
	Receive(ctx context.Context, entries []yacymodel.RWIPosting) (Receipt, error)
}

type Receipt struct {
	Busy       bool
	Pause      int
	UnknownURL []yacymodel.Hash
}

type Config struct {
	BatchCap     int
	PauseSeconds int
}

func Open(
	vault *boltvault.Vault,
	urls urlmeta.URLDirectory,
	cfg Config,
) (PostingIndex, PostingReceiver, error) {
	postings, err := registerPostings(vault)
	if err != nil {
		return nil, nil, err
	}
	references, err := registerReferences(vault)
	if err != nil {
		return nil, nil, err
	}

	directory := postingDirectory{vault: vault, postings: postings, references: references}
	intake := postingIntake{
		vault:        vault,
		postings:     postings,
		references:   references,
		urls:         urls,
		batchCap:     cfg.BatchCap,
		pauseSeconds: cfg.PauseSeconds,
	}

	return directory, intake, nil
}

func MountTransferRWI(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	receiver PostingReceiver,
) {
	httpguard.Mount(
		router,
		yacyproto.PathTransferRWI,
		yacyproto.TransferRWIEndpointMethods,
		yacyproto.ParseTransferRWIRequest,
		transferRWIEndpoint{identity: identity, intake: receiver}.Serve,
	)
}
