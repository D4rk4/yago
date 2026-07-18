package crawlorder

import (
	"bytes"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type activeOrderDefinition struct {
	exactIdentity   []byte
	contentIdentity []byte
	priority        yagocrawlcontract.CrawlOrderPriority
}

func identifyActiveOrder(delivery CrawlOrderDelivery) (activeOrderDefinition, bool) {
	exactIdentity, err := crawlOrderDeliveryIdentity(delivery)
	if err != nil {
		return activeOrderDefinition{}, false
	}
	contentIdentity, err := crawlOrderIdentity(delivery.Order)
	if err != nil {
		return activeOrderDefinition{}, false
	}

	return activeOrderDefinition{
		exactIdentity:   exactIdentity,
		contentIdentity: contentIdentity,
		priority:        delivery.Order.Priority,
	}, true
}

func (definition activeOrderDefinition) matches(other activeOrderDefinition) bool {
	return bytes.Equal(definition.exactIdentity, other.exactIdentity) &&
		bytes.Equal(definition.contentIdentity, other.contentIdentity) &&
		definition.priority == other.priority
}
