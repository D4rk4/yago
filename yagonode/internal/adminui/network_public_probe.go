package adminui

import "context"

type NetworkSelfTester interface {
	TestPublicEndpoint(ctx context.Context) (NetworkStatus, error)
}
