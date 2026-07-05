package crawldispatch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

type restartQueue struct {
	orders []yagocrawlcontract.CrawlOrder
}

func (q *restartQueue) PublishOnce(
	_ context.Context,
	_ string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.orders = append(q.orders, order)

	return false, nil
}

func testMint(t *testing.T) crawldispatch.ProvenanceMint {
	t.Helper()
	counter := byte(0)

	return func() []byte {
		counter++

		return []byte{counter}
	}
}

func restartRequest() crawldispatch.OperatorRequest {
	return crawldispatch.OperatorRequest{
		Name:               "restartable",
		Seeds:              []string{"https://example.org/"},
		StartMode:          "url",
		Scope:              "domain",
		MaxDepth:           1,
		MaxPagesPerHost:    yagocrawlcontract.UnlimitedPagesPerHost,
		IgnoreTLSAuthority: true,
	}
}

func TestDispatcherRestartRepublishesTheLastOrderForTheProfile(t *testing.T) {
	queue := &restartQueue{}
	dispatcher := crawldispatch.NewDispatcher("0123456789AB", testMint(t), queue)

	accepted, err := dispatcher.Dispatch(t.Context(), restartRequest(), "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	restarted, err := dispatcher.Restart(t.Context(), accepted.ProfileHandle)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.ProfileHandle != accepted.ProfileHandle || len(queue.orders) != 2 {
		t.Fatalf("restarted = %+v orders = %d", restarted, len(queue.orders))
	}
	if !queue.orders[1].Profile.IgnoreTLSAuthority {
		t.Fatal("restarted order lost the profile's TLS-authority opt-out")
	}
	if string(queue.orders[0].Provenance) == string(queue.orders[1].Provenance) {
		t.Fatal("restart must mint a fresh provenance for a new run")
	}
}

func TestDispatcherRestartRejectsUnknownProfile(t *testing.T) {
	dispatcher := crawldispatch.NewDispatcher("0123456789AB", testMint(t), &restartQueue{})

	if _, err := dispatcher.Restart(
		t.Context(),
		"UnknownHandle",
	); !errors.Is(err, crawldispatch.ErrNoRestartableOrder) {
		t.Fatalf("Restart error = %v, want ErrNoRestartableOrder", err)
	}
}
