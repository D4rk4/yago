package frontier

import (
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

// ValueScorer predicts how much index value fetching a ready job yields, given
// how many pages its host already contributed to the run; the frontier
// dispatches the highest-scored due job first, so a limited page budget is
// spent on high-value pages (Craw4LLM, arXiv:2502.13347, shows value-ordered
// frontiers reach benchmark quality on a fraction of the fetches). An external
// scorer plugs in through WithValueScorer; nodes without one keep the built-in
// heuristic.
type ValueScorer func(job crawljob.CrawlJob, hostPagesSeen int) float64

const (
	// hostNoveltyWeight rewards hosts that contributed few pages so far, keeping
	// runs broad instead of drilling one site to its depth limit first.
	hostNoveltyWeight = 0.5
	// urlShapePenalty is the per-segment and per-parameter deduction: deep
	// paths and parameter-heavy URLs are disproportionately pagination,
	// calendars, and faceted listings.
	urlShapePenalty = 0.05
)

// DefaultValueScorer is the model-free heuristic: shallow link-distance first
// (the classic quality signal behind breadth-first ordering), novel hosts
// next, long paths and query tails last.
func DefaultValueScorer(job crawljob.CrawlJob, hostPagesSeen int) float64 {
	score := 1.0/(1.0+float64(job.Depth)) +
		hostNoveltyWeight/(1.0+float64(hostPagesSeen))
	parsed, err := url.Parse(job.URL)
	if err != nil {
		return score
	}
	segments := len(strings.FieldsFunc(parsed.Path, func(r rune) bool { return r == '/' }))
	parameters := 0
	if parsed.RawQuery != "" {
		parameters = strings.Count(parsed.RawQuery, "&") + 1
	}

	return score - urlShapePenalty*float64(segments+parameters)
}

// WithValueScorer replaces the built-in dispatch-value heuristic. A nil scorer
// is ignored.
func WithValueScorer(scorer ValueScorer) Option {
	return func(f *Frontier) {
		if scorer != nil {
			f.scorer = scorer
		}
	}
}

// jobValueLocked scores a ready job with the run's current host tally; callers
// hold f.mu.
func (f *Frontier) jobValueLocked(job crawljob.CrawlJob) float64 {
	hostPages := 0
	if run, ok := f.state.runs[job.RunID]; ok {
		hostPages = run.hostPages[weburl.Host(job.URL)]
	}

	return f.scorer(job, hostPages)
}
