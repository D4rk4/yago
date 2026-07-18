package crawlorder

import (
	"bytes"
	"crypto/sha256"
	"errors"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

var marshalCrawlOrderIdentity = yagocrawlcontract.MarshalCrawlOrder

var errInvalidCrawlOrderIdentity = errors.New("crawl order identity must contain 32 bytes")

func crawlOrderDeliveryIdentity(delivery CrawlOrderDelivery) ([]byte, error) {
	if len(delivery.OrderIdentity) == 0 {
		return crawlOrderIdentity(delivery.Order)
	}
	if len(delivery.OrderIdentity) != sha256.Size {
		return nil, errInvalidCrawlOrderIdentity
	}

	return bytes.Clone(delivery.OrderIdentity), nil
}

func crawlOrderPayloadIdentity(encoded []byte) []byte {
	identity := sha256.Sum256(encoded)

	return identity[:]
}

func crawlOrderIdentity(order yagocrawlcontract.CrawlOrder) ([]byte, error) {
	encoded, err := marshalCrawlOrderIdentity(order)
	if err != nil {
		return nil, err
	}

	return crawlOrderPayloadIdentity(encoded), nil
}
