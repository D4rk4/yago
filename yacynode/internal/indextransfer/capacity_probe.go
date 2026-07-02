package indextransfer

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacyproto"
)

var (
	ErrCapacityProbeRejected = errors.New("capacity probe rejected")
	errCapacityProbeFailed   = errors.New("capacity probe failed")
)

type RemoteRWICountProbe struct {
	client      *http.Client
	networkName string
	self        yacymodel.Seed
}

func NewRemoteRWICountProbe(
	client *http.Client,
	networkName string,
	self yacymodel.Seed,
) RemoteRWICountProbe {
	if client == nil {
		client = http.DefaultClient
	}

	return RemoteRWICountProbe{client: client, networkName: networkName, self: self}
}

func (p RemoteRWICountProbe) RWICount(
	ctx context.Context,
	peer yacymodel.Seed,
) (int, error) {
	resp, err := postTransfer(
		transferPost[yacyproto.QueryResponse]{
			ctx:    ctx,
			client: p.client,
			peer:   peer,
			path:   yacyproto.PathQuery,
			form: yacyproto.QueryRequest{
				NetworkName: p.networkName,
				YouAre:      peer.Hash,
				Iam:         p.self.Hash,
				Object:      yacyproto.ObjectRWICount,
			}.Form(),
			parse: yacyproto.ParseQueryResponse,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errCapacityProbeFailed, err)
	}
	if resp.Response == yacyproto.QueryResponseRejected {
		return 0, fmt.Errorf("%w: %w", errCapacityProbeFailed, ErrCapacityProbeRejected)
	}
	if resp.Response < 0 {
		return 0, fmt.Errorf("%w: negative response %d", errCapacityProbeFailed, resp.Response)
	}

	return resp.Response, nil
}
