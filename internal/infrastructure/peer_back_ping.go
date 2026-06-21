package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const backPingMaxBodyBytes int64 = 64 << 10

var ErrBackPingUnreachable = errors.New("peer back-ping unreachable")

type PeerBackPing struct {
	client      *http.Client
	self        yacymodel.Hash
	networkName string
}

func NewPeerBackPing(client *http.Client, self yacymodel.Hash, networkName string) *PeerBackPing {
	return &PeerBackPing{
		client:      client,
		self:        self,
		networkName: networkName,
	}
}

func (p *PeerBackPing) Ping(ctx context.Context, peer yacymodel.Seed) error {
	target, err := backPingURL(peer)
	if err != nil {
		return err
	}

	query := yacyproto.QueryRequest{
		NetworkName: p.networkName,
		YouAre:      peer.Hash,
		Iam:         p.self,
		Object:      yacyproto.ObjectRWICount,
	}
	target.RawQuery = query.Form().Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}
	defer closeResponseBody(ctx, resp.Body, "peerBackPing")

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrBackPingUnreachable, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, backPingMaxBodyBytes))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	msg, err := yacymodel.ParseMessage(string(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}
	if _, err := yacyproto.ParseQueryResponse(msg); err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	return nil
}

func backPingURL(peer yacymodel.Seed) (*url.URL, error) {
	target, err := peer.HTTPEndpoint(yacyproto.PathQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	return target, nil
}

var _ ports.PeerPinger = (*PeerBackPing)(nil)
