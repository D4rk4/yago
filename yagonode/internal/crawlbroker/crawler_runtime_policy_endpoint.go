package crawlbroker

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (s *exchangeServer) ReadRuntimePolicy(
	_ context.Context,
	request *crawlrpc.CrawlerRuntimePolicyRequest,
) (*crawlrpc.CrawlerRuntimePolicy, error) {
	if !yagocrawlcontract.ValidCrawlerWorkerIdentity(request.GetWorkerId()) {
		return nil, status.Error(codes.InvalidArgument, "invalid crawler worker identity")
	}
	policy, err := crawlerStartupRuntimePolicy(
		s.control.RuntimePolicy(),
		s.control.StoragePressurePolicy(),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "invalid crawler runtime policy")
	}

	return policy, nil
}
