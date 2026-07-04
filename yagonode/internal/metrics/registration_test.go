package metrics

import "testing"

func TestCollectorsShareRegistryWithoutConflict(t *testing.T) {
	endpoints := NewHTTPEndpointMetrics()
	registry := endpoints.Registry()

	NewAuthMetrics(registry)
	NewEvictionMetrics(registry)
	NewDHTOutboundMetrics(registry)
	NewDHTInboundMetrics(registry)
	NewPeerMetrics(registry)
	NewSearchMetrics(registry)
	NewStorageMetrics(registry, stubStorage{})
	NewQueueDepthMetrics(registry, stubQueue{})

	if _, err := registry.Gather(); err != nil {
		t.Fatalf("gather after registering every collector: %v", err)
	}
}
