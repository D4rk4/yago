package rwi

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type Config struct {
	BatchCap     int
	PauseSeconds int
}

type Module struct {
	Directory   PostingDirectory
	Index       PostingScanner
	TransferRWI http.Handler
	intake      postingIntake
}

func (m Module) Intake(ctx context.Context, entries []yacymodel.RWIPosting) (Receipt, error) {
	return m.intake.Receive(ctx, entries)
}

func New(
	vault *boltvault.Vault,
	guard httpguard.RequestGuard,
	respond httpguard.WireResponder,
	urls urlmeta.URLDirectory,
	cfg Config,
) (Module, error) {
	postings, err := registerPostings(vault)
	if err != nil {
		return Module{}, err
	}
	references, err := registerReferences(vault)
	if err != nil {
		return Module{}, err
	}

	intake := postingIntake{
		vault:        vault,
		postings:     postings,
		references:   references,
		urls:         urls,
		batchCap:     cfg.BatchCap,
		pauseSeconds: cfg.PauseSeconds,
	}
	directory := postingDirectory{vault: vault, postings: postings, references: references}

	// FIXME: register the transferRWI handler with a shared router here (mirroring
	// registerPostings) instead of returning it in Module for cmd to mount.
	return Module{
		Directory:   directory,
		Index:       directory,
		TransferRWI: transferRWIEndpoint{guard: guard, respond: respond, intake: intake},
		intake:      intake,
	}, nil
}
