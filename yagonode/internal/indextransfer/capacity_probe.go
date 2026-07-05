package indextransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
}

func NewRemoteRWICountProbe(
	client *http.Client,
	networkName string,
	self yagomodel.Seed,
	preferHTTPS bool,
) RemoteRWICountProbe {
	if client == nil {
		client = http.DefaultClient
	}

	return RemoteRWICountProbe{
		client:      client,
		networkName: networkName,
		self:        self,
		preferHTTPS: preferHTTPS,
	}
}

func (p RemoteRWICountProbe) RWICount(
	ctx context.Context,
	peer yagomodel.Seed,
) (int, error) {
	resp, err := postTransfer(
		transferPost[yagoproto.QueryResponse]{
			ctx:    ctx,
			client: p.client,
			peer:   peer,
			path:   yagoproto.PathQuery,
			form: yagoproto.QueryRequest{
				NetworkName: p.networkName,
				YouAre:      peer.Hash,
				Iam:         p.self.Hash,
				Object:      yagoproto.ObjectRWICount,
			}.Form(),
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
