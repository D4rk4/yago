package adminui

var automaticDiscoverySettingKeys = map[string]bool{
	"swarm.seed.enabled":                     true,
	"swarm.seed.depth":                       true,
	"swarm.seed.max_pages":                   true,
	"web.fallback.seed_crawl":                true,
	"web.fallback.seed_depth":                true,
	"web.fallback.seed_max_pages":            true,
	"autocrawler.crawl.query_urls":           true,
	"autocrawler.crawl.tls_insecure":         true,
	"autocrawler.crawl.ignore_robots":        true,
	"autocrawler.crawl.no_browser":           true,
	"autocrawler.crawl.follow_nofollow":      true,
	"autocrawler.crawl.recrawl_interval":     true,
	"crawler.prioritize_automatic_discovery": true,
}

func configurationSettingGroups(items []SettingItem) []SettingGroup {
	presentation := make([]SettingItem, len(items))
	copy(presentation, items)
	for index := range presentation {
		if automaticDiscoverySettingKeys[presentation[index].Key] {
			presentation[index].Category = "Crawler"
		}
	}
	groups := groupSettings(presentation)
	for index := range groups {
		if groups[index].Title != "Crawler" {
			continue
		}
		crawlerItems := make([]SettingItem, 0, len(groups[index].Items))
		automaticItems := make([]SettingItem, 0, len(groups[index].Items))
		for _, item := range groups[index].Items {
			if automaticDiscoverySettingKeys[item.Key] {
				automaticItems = append(automaticItems, item)
			} else {
				crawlerItems = append(crawlerItems, item)
			}
		}
		fieldsets := make([]SettingFieldset, 0, 2)
		if len(crawlerItems) > 0 {
			fieldsets = append(fieldsets, SettingFieldset{
				Title: "Crawler",
				Items: crawlerItems,
			})
		}
		if len(automaticItems) > 0 {
			fieldsets = append(fieldsets, SettingFieldset{
				Title: "Automatic discovery",
				Items: automaticItems,
			})
		}
		groups[index].Fieldsets = fieldsets
	}

	return groups
}
