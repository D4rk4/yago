package crawlbroker

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestCrawlerDataExchangeRequiresValidAPIKey(t *testing.T) {
	server := newCrawlerDataExchangeServer(CrawlerDataExchangeConfig{
		AuthorizeKey: func(_ context.Context, key string) bool { return key == "accepted-key" },
	}, nil, nil)
	tests := []struct {
		name    string
		context context.Context
		want    codes.Code
	}{
		{name: "missing", context: context.Background(), want: codes.Unauthenticated},
		{
			name: "rejected",
			context: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"authorization", "Bearer rejected-key",
			)),
			want: codes.Unauthenticated,
		},
		{
			name: "accepted",
			context: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"authorization", "Bearer accepted-key",
			)),
			want: codes.OK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := server.authorize(test.context); status.Code(err) != test.want {
				t.Fatalf("authorize code = %s, want %s", status.Code(err), test.want)
			}
		})
	}
}

func TestCrawlerDataExchangeRejectsMutationWithoutIdentity(t *testing.T) {
	server := newCrawlerDataExchangeServer(CrawlerDataExchangeConfig{}, nil, nil)
	_, err := server.absorbMutation(context.Background(), &crawlrpc.DocumentMutation{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("absorbMutation code = %s, want %s", status.Code(err), codes.InvalidArgument)
	}
}
