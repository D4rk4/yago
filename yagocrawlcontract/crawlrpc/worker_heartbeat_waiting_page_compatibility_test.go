package crawlrpc_test

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestWorkerHeartbeatURLActivityFieldsAreAdditive(t *testing.T) {
	current := &crawlrpc.WorkerHeartbeat{
		WorkerId: "current",
		ActivePages: []*crawlrpc.ActiveCrawlPage{{
			RunId: "run-a", Url: "https://example.org/current", Stage: "http_fetch",
		}},
		WaitingPages: []*crawlrpc.WaitingCrawlPage{{
			RunId: "run-a", Url: "https://example.org/next", State: "queued",
			Reason: "queued", AvailableAfterUnixMilliseconds: 1_800_000_000_000,
		}},
	}
	wire, err := proto.Marshal(current)
	if err != nil {
		t.Fatalf("marshal crawler URL activity: %v", err)
	}
	decoded := &crawlrpc.WorkerHeartbeat{}
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatalf("unmarshal crawler URL activity: %v", err)
	}
	if !proto.Equal(decoded, current) {
		t.Fatalf("decoded crawler URL activity = %+v", decoded)
	}
	legacy := dynamicpb.NewMessage(legacyWorkerHeartbeatDescriptor(t))
	if err := proto.Unmarshal(wire, legacy); err != nil {
		t.Fatalf("legacy heartbeat rejected crawler URL activity: %v", err)
	}
}
