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

type StatusSnapshot struct {
	Version     string
	Uptime      int
	NetworkName string
	Seed        yacymodel.Seed
}

type RuntimeStatus interface {
	Snapshot(ctx context.Context) StatusSnapshot
}

type helloEndpoint struct {
	guard          httpguard.RequestGuard
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

	snapshot := e.status.Snapshot(ctx)
	resp := yacyproto.HelloResponse{
		ResponseHeader: yacyproto.ResponseHeader{
			Version: snapshot.Version,
			Uptime:  snapshot.Uptime,
		},
		YourIP: clientAddress(r, e.trustedProxies),
		Seeds:  []yacymodel.Seed{snapshot.Seed},
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
	httpguard.WriteWireMessage(ctx, w, resp.Encode())
}
