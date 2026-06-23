package peering

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type helloDirectory interface {
	Hello(ctx context.Context, caller yacymodel.Seed, count int) (helloOutcome, error)
}

type helloEndpoint struct {
	peer   httpguard.PeerIdentity
	status RuntimeStatus
	peers  helloDirectory
}

func (e helloEndpoint) Serve(
	ctx context.Context,
	req yacyproto.HelloRequest,
) (yacyproto.HelloResponse, error) {
	resp := yacyproto.HelloResponse{
		YourIP: httpguard.RemoteAddr(ctx),
		Seeds:  []yacymodel.Seed{e.status.SelfSeed(ctx)},
	}

	if e.peer.NetworkMatches(req.NetworkName) {
		outcome, err := e.peers.Hello(ctx, req.Seed, req.Count)
		if err != nil {
			return yacyproto.HelloResponse{}, fmt.Errorf("hello: %w", err)
		}

		resp.YourType = outcome.CallerType
		resp.Seeds = append(resp.Seeds, outcome.Known...)
	}

	slog.DebugContext(ctx, "hello served", slog.Int("seedCount", len(resp.Seeds)))

	return resp, nil
}
