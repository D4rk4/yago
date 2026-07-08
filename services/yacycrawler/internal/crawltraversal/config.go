package crawltraversal

import "time"

type Config struct {
	RunPageBudget       int
	FrontierCapacity    int
	FetchRetryLimit     int
	FetchRetryFloor     time.Duration
	FetchRetryCeiling   time.Duration
	PublishRetryFloor   time.Duration
	PublishRetryCeiling time.Duration
	MaxDeferralsPerURL  int
	FetchConcurrency    int
	OwnershipInterval   time.Duration
}
