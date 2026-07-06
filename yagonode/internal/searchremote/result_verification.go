package searchremote

import (
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

// cappedPeerRows bounds one peer's answer to the row count the request asked
// for, YaCy parity: its remote-search ingest processes no more than requested
// "in case that evil peers fill us up with rubbish"
// (Protocol.remoteSearchProcess).
func cappedPeerRows(
	rows []yagomodel.URIMetadataRow,
	limit int,
) []yagomodel.URIMetadataRow {
	if limit > 0 && len(rows) > limit {
		return rows[:limit]
	}

	return rows
}

// verifiedPeerResults keeps the peer rows whose own metadata — the title,
// snippet, and URL the peer itself sent — mentions at least one query term,
// and drops the rest as spam, YaCy parity twice over: an honest peer runs the
// all-words conjunction over its index before answering (search.java
// searchConjunction), and YaCy's snippet verification sorts out results with
// no matching text (TextSnippet ERROR_NO_MATCH). Verify=false trusts peers
// verbatim, mirroring YaCy's verify=false.
func verifiedPeerResults(
	req searchcore.Request,
	results []searchcore.Result,
) []searchcore.Result {
	if req.Verify == searchcore.VerifyFalse {
		return results
	}
	terms := verificationTerms(req)
	kept := make([]searchcore.Result, 0, len(results))
	for _, result := range results {
		if searchcore.ResultMentionsTerms(result, terms) {
			kept = append(kept, result)
		}
	}

	return kept
}

// verificationTerms is the content words of the query — a peer row must
// mention a word that carries meaning, never verify on a bare function word
// («что» appears in half the Russian web), matching YaCy's snippet check of
// the include words minus stopwords. An all-stopword query falls back to the
// full term list so something is still checked.
func verificationTerms(req searchcore.Request) []string {
	terms := req.Terms
	if len(terms) == 0 {
		terms = strings.Fields(req.Query)
	}
	if content := stopwords.ContentTerms(terms); len(content) > 0 {
		return content
	}

	return terms
}
