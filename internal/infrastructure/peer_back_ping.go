package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

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

	youAre, err := peer.Hash()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	query := yacyproto.QueryRequest{
		NetworkName: p.networkName,
		YouAre:      youAre,
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrBackPingUnreachable, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, backPingMaxBodyBytes))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	if _, err := yacyproto.ParseQueryResponse(yacymodel.ParseMessage(string(body))); err != nil {
		return fmt.Errorf("%w: %w", ErrBackPingUnreachable, err)
	}

	return nil
}

func backPingURL(peer yacymodel.Seed) (*url.URL, error) {
	host := peer[yacymodel.SeedIP]
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("%w: bad ip %q", ErrBackPingUnreachable, host)
	}

	port, err := peer.Port()
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("%w: bad port", ErrBackPingUnreachable)
	}

	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   yacyproto.PathQuery,
	}, nil
}

var _ ports.PeerPinger = (*PeerBackPing)(nil)
