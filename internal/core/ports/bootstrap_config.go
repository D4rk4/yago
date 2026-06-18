package ports

import "time"

type BootstrapConfig interface {
	SeedlistURLs() []string
	BootstrapPeers() []string
	AnnounceInterval() time.Duration
}
