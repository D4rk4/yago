package yagocrawlcontract

type CrawlOrderPriority string

const (
	CrawlOrderPriorityNormal             CrawlOrderPriority = ""
	CrawlOrderPriorityAutomaticDiscovery CrawlOrderPriority = "automatic_discovery"
	AutomaticDiscoveryPriorityBurst                         = 3
)
