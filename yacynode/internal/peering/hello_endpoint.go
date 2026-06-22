package peering

import (
	"context"
	"log/slog"
	"net"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type RuntimeStatus interface {
	NetworkName(ctx context.Context) string
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type helloEndpoint struct {
	guard          httpguard.RequestGuard
	respond        httpguard.WireResponder
	status         RuntimeStatus
	peers          PeerDirectory
	trustedProxies []*net.IPNet
}

func (e helloEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.HelloEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseHelloRequest(ctx, form)
	if err != nil {
		httpguard.FailBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.HelloResponse{
		YourIP: clientAddress(r, e.trustedProxies),
		Seeds:  []yacymodel.Seed{e.status.SelfSeed(ctx)},
	}

	if e.guard.NetworkMatches(form) {
		outcome, err := e.peers.Hello(ctx, req.Seed, req.Count)
		if err != nil {
			httpguard.FailInternal(ctx, w, "hello failed", err)

			return
		}

		resp.YourType = outcome.CallerType
		resp.Seeds = append(resp.Seeds, outcome.Known...)
	}

	slog.DebugContext(ctx, "hello served", slog.Int("seedCount", len(resp.Seeds)))
	e.respond.Write(ctx, w, resp.Encode())
}
