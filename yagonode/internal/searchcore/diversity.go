package searchcore

import "strings"

const (
	// hostCrowdingCap is how many results one host may hold in the upper
	// ranks before its remaining hits are deferred behind other hosts —
	// YaCy's skipDoubleDom/doubleDomCache pull, bounded like major engines'
	// ~two-per-domain host crowding.
	hostCrowdingCap = 2
)

func DiversifyResults(results []Result, req Request) []Result {
	consolidated := ConsolidateClusters(results)
	if req.SiteHost != "" || req.SortByDate {
		return consolidated
	}

	return deferCrowdedHosts(rerankMarginalRelevance(consolidated))
}

// deferCrowdedHosts keeps at most hostCrowdingCap results per host in place
// and appends the overflow afterwards, both sides in their original order.
func deferCrowdedHosts(results []Result) []Result {
	counts := make(map[string]int, len(results))
	head := make([]Result, 0, len(results))
	var overflow []Result
	for _, result := range results {
		host := strings.ToLower(result.Host)
		if host != "" && counts[host] >= hostCrowdingCap {
			overflow = append(overflow, result)

			continue
		}
		counts[host]++
		head = append(head, result)
	}

	return append(head, overflow...)
}

// simhash builds a 64-bit fingerprint from lowercase content-word tokens; the
// second return reports whether the text carried enough tokens to compare.
// Function words are excluded: short query-biased snippets are dominated by
// them, and fingerprints built over «что»/«как»/"the"-grade tokens collide at
// the near-duplicate threshold for texts that share no content at all, so a
// relevant result could vanish as a false "duplicate" of an unrelated one.
