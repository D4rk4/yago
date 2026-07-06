package searchcore

import (
	"hash/fnv"
	"math/bits"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	// hostCrowdingCap is how many results one host may hold in the upper
	// ranks before its remaining hits are deferred behind other hosts —
	// YaCy's skipDoubleDom/doubleDomCache pull, bounded like major engines'
	// ~two-per-domain host crowding.
	hostCrowdingCap = 2
	// nearDuplicateMaxDistance is the SimHash Hamming distance at or under
	// which two result texts count as near-duplicates.
	nearDuplicateMaxDistance = 3
	// nearDuplicateMinTokens keeps short texts out of SimHash comparison:
	// with only a few tokens the fingerprint is too sparse to mean anything
	// and empty snippets would all "match" each other.
	nearDuplicateMinTokens = 8
)

// DiversifyResults drops near-duplicate results (SimHash over title and
// snippet), defers host-crowding overflow — hits beyond the per-host cap
// move behind other hosts in stable order, they are not dropped — and then
// reorders the visible window with Maximal Marginal Relevance so texts that
// repeat an already-chosen result yield to novel ones. Crowding and MMR are
// skipped for site: queries (one host is the point) and for /date ordering
// (reordering would break the chronology); deduplication always applies.
// Runs before the paging window is cut so pages stay stable.
func DiversifyResults(results []Result, req Request) []Result {
	deduped := dropNearDuplicates(results)
	if req.SiteHost != "" || req.SortByDate {
		return deduped
	}

	return rerankMarginalRelevance(deferCrowdedHosts(deduped))
}

func dropNearDuplicates(results []Result) []Result {
	kept := make([]Result, 0, len(results))
	fingerprints := make([]uint64, 0, len(results))
	for _, result := range results {
		fingerprint, comparable := simhash(result.Title + " " + result.Snippet)
		duplicate := false
		if comparable {
			for _, seen := range fingerprints {
				if bits.OnesCount64(fingerprint^seen) <= nearDuplicateMaxDistance {
					duplicate = true

					break
				}
			}
		}
		if duplicate {
			continue
		}
		if comparable {
			fingerprints = append(fingerprints, fingerprint)
		}
		kept = append(kept, result)
	}

	return kept
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
func simhash(text string) (uint64, bool) {
	var weights [64]int
	tokens := 0
	for _, token := range strings.Fields(strings.ToLower(text)) {
		if stopwords.IsStopword(strings.Trim(token, ".,!?…:;\"'()[]«»—-")) {
			continue
		}
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		tokenHash := hasher.Sum64()
		for bit := range weights {
			if tokenHash&(1<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
		tokens++
	}
	if tokens < nearDuplicateMinTokens {
		return 0, false
	}

	var fingerprint uint64
	for bit, weight := range weights {
		if weight > 0 {
			fingerprint |= 1 << bit
		}
	}

	return fingerprint, true
}
