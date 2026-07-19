package indextransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

var (
	ErrCapacityProbeRejected = errors.New("capacity probe rejected")
	errCapacityProbeFailed   = errors.New("capacity probe failed")
)

type RemoteRWICountProbe struct {
	client      *http.Client
	networkName string
	self        yagomodel.Seed
	preferHTTPS bool
	access      yagoproto.NetworkAccess
	signForm    func(url.Values) error
}

func NewRemoteRWICountProbe(
	client *http.Client,
	networkName string,
	self yagomodel.Seed,
	preferHTTPS bool,
	access ...yagoproto.NetworkAccess,
) RemoteRWICountProbe {
	if client == nil {
		client = http.DefaultClient
	}

	configured := yagoproto.NetworkAccess{NetworkName: networkName, Self: self.Hash}
	if len(access) != 0 {
		configured = access[0]
		configured.Self = self.Hash
	}

	return RemoteRWICountProbe{
		client:      client,
		networkName: networkName,
		self:        self,
		preferHTTPS: preferHTTPS,
		access:      configured,
		signForm:    configured.Sign,
	}
}

func (p RemoteRWICountProbe) RWICount(
	ctx context.Context,
	peer yagomodel.Seed,
) (int, error) {
	form := yagoproto.QueryRequest{
		NetworkName: p.networkName,
		YouAre:      peer.Hash,
		Iam:         p.self.Hash,
		Object:      yagoproto.ObjectRWICount,
	}.Form()
	if p.access.Mode == yagoproto.NetworkAuthenticationSaltedMagic {
		if err := p.signForm(form); err != nil {
			return 0, fmt.Errorf("%w: %w", errCapacityProbeFailed, err)
		}
	}
	resp, err := postTransfer(
		transferPost[yagoproto.QueryResponse]{
			ctx:         ctx,
			client:      p.client,
			peer:        peer,
			path:        yagoproto.PathQuery,
			form:        form,
			parse:       yagoproto.ParseQueryResponse,
			preferHTTPS: p.preferHTTPS,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errCapacityProbeFailed, err)
	}
	if resp.Response == yagoproto.QueryResponseRejected {
		return 0, fmt.Errorf("%w: %w", errCapacityProbeFailed, ErrCapacityProbeRejected)
	}
	if resp.Response < 0 {
		return 0, fmt.Errorf("%w: negative response %d", errCapacityProbeFailed, resp.Response)
	}

	return resp.Response, nil
}
