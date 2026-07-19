package tavilyapi

import "strings"

const (
	extractionsPerUsageCredit = 5
	mappedPagesPerUsageCredit = 10
)

func responseUsage(enabled bool, credits int) *SearchUsage {
	if !enabled {
		return nil
	}

	return &SearchUsage{Credits: credits}
}

func searchResponseUsage(req SearchRequest, executed bool) *SearchUsage {
	credits := 0
	if executed {
		credits = usageDepthMultiplier(req.SearchDepth)
	}

	return responseUsage(req.IncludeUsage, credits)
}

func extractResponseUsage(req ExtractRequest, successfulExtractions int) *SearchUsage {
	credits := successfulExtractions / extractionsPerUsageCredit
	credits *= usageDepthMultiplier(req.ExtractDepth)

	return responseUsage(req.IncludeUsage, credits)
}

func mapResponseUsage(req CrawlRequest, successfulPages int) *SearchUsage {
	return responseUsage(req.IncludeUsage, mappingUsageCredits(req, successfulPages))
}

func crawlResponseUsage(req CrawlRequest, successfulPages int) *SearchUsage {
	credits := mappingUsageCredits(req, successfulPages)
	credits += successfulPages / extractionsPerUsageCredit * usageDepthMultiplier(req.ExtractDepth)

	return responseUsage(req.IncludeUsage, credits)
}

func mappingUsageCredits(req CrawlRequest, successfulPages int) int {
	credits := successfulPages / mappedPagesPerUsageCredit
	if strings.TrimSpace(req.Instructions) != "" {
		credits *= 2
	}

	return credits
}

func usageDepthMultiplier(depth string) int {
	if strings.EqualFold(strings.TrimSpace(depth), "advanced") {
		return 2
	}

	return 1
}
