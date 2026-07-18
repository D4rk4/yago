package documentsearch

import (
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func negotiatedAnalyzerRequirementsBound(request yagoproto.SearchRequest) bool {
	if len(request.URLs) > 0 {
		return true
	}
	if len(request.Query) == 0 || len(request.Query) != len(request.EvidenceTerms) {
		return false
	}
	remaining := make(map[yagomodel.Hash]int, len(request.Query))
	for _, hash := range request.Query {
		remaining[hash]++
	}
	for _, term := range request.EvidenceTerms {
		hash := yagomodel.WordHash(strings.TrimSpace(term))
		if remaining[hash] == 0 {
			return false
		}
		remaining[hash]--
	}

	return true
}
