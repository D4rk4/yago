package websearch

func resultURLs(results []Result, admission crawlSeedURLAdmission) []string {
	urls := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		normalized, admitted := admission.AdmitCrawlSeedURL(result.URL)
		if !admitted {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		urls = append(urls, normalized)
	}

	return urls
}
