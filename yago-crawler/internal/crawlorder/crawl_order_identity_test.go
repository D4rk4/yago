package crawlorder

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestCrawlOrderIdentityIsStableAndContentBound(t *testing.T) {
	order := identityTestOrder()
	first, err := crawlOrderIdentity(order)
	if err != nil {
		t.Fatalf("first identity: %v", err)
	}
	second, err := crawlOrderIdentity(order)
	if err != nil {
		t.Fatalf("second identity: %v", err)
	}
	if string(first) != string(second) || len(first) != 32 {
		t.Fatalf("stable identity = %x, %x", first, second)
	}
	order.Requests = append(order.Requests, yagocrawlcontract.CrawlRequest{
		URL:           "https://example.com/other",
		ProfileHandle: order.Profile.Handle,
	})
	changed, err := crawlOrderIdentity(order)
	if err != nil {
		t.Fatalf("changed identity: %v", err)
	}
	if string(first) == string(changed) {
		t.Fatal("different orders share an identity")
	}
}

func TestCrawlOrderIdentityReturnsMarshalFailure(t *testing.T) {
	saved := marshalCrawlOrderIdentity
	t.Cleanup(func() { marshalCrawlOrderIdentity = saved })
	sentinel := errors.New("marshal failed")
	marshalCrawlOrderIdentity = func(yagocrawlcontract.CrawlOrder) ([]byte, error) {
		return nil, sentinel
	}
	if _, err := crawlOrderIdentity(identityTestOrder()); !errors.Is(err, sentinel) {
		t.Fatalf("identity error = %v, want %v", err, sentinel)
	}
}

func TestCrawlOrderDeliveryIdentityUsesExactPayloadValue(t *testing.T) {
	payloadIdentity := sha256.Sum256([]byte(`{"Profile":{"Name":"historical"}}`))
	delivery := CrawlOrderDelivery{
		Order:         identityTestOrder(),
		OrderIdentity: payloadIdentity[:],
	}
	identity, err := crawlOrderDeliveryIdentity(delivery)
	if err != nil {
		t.Fatalf("delivery identity: %v", err)
	}
	originalFirstByte := delivery.OrderIdentity[0]
	identity[0] ^= 0xff
	if delivery.OrderIdentity[0] != originalFirstByte || identity[0] == originalFirstByte {
		t.Fatal("delivery identity was not detached")
	}
}

func TestCrawlOrderDeliveryIdentityFallsBackForSyntheticDelivery(t *testing.T) {
	delivery := CrawlOrderDelivery{Order: identityTestOrder()}
	want, err := crawlOrderIdentity(delivery.Order)
	if err != nil {
		t.Fatalf("fallback identity: %v", err)
	}
	got, err := crawlOrderDeliveryIdentity(delivery)
	if err != nil {
		t.Fatalf("delivery identity: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("delivery identity = %x, want %x", got, want)
	}
}

func TestCrawlOrderDeliveryIdentityRejectsInvalidExactValue(t *testing.T) {
	_, err := crawlOrderDeliveryIdentity(CrawlOrderDelivery{OrderIdentity: []byte("short")})
	if !errors.Is(err, errInvalidCrawlOrderIdentity) {
		t.Fatalf("delivery identity error = %v, want %v", err, errInvalidCrawlOrderIdentity)
	}
}

func identityTestOrder() yagocrawlcontract.CrawlOrder {
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})

	return yagocrawlcontract.CrawlOrder{
		Provenance: []byte("identity"),
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           "https://example.com/",
			ProfileHandle: profile.Handle,
		}},
	}
}
