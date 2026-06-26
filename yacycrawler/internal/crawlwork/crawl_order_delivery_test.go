package crawlwork_test

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
)

func TestNewCrawlOrderDeliveryHasNoopActions(t *testing.T) {
	order := yacycrawlcontract.CrawlOrder{Provenance: []byte("admin")}
	delivery := crawlwork.NewCrawlOrderDelivery(order)

	if string(delivery.Order.Provenance) != "admin" {
		t.Errorf("order not carried, got %q", delivery.Order.Provenance)
	}
	ctx := context.Background()
	for name, action := range map[string]func(context.Context) error{
		"ack":  delivery.Ack,
		"nak":  delivery.Nak,
		"term": delivery.Term,
	} {
		if err := action(ctx); err != nil {
			t.Errorf("%s returned %v, want nil", name, err)
		}
	}
}
