package ports

import "time"

type BootstrapConfig interface {
	SeedlistURLs() []string
	AnnounceInterval() time.Duration
}
