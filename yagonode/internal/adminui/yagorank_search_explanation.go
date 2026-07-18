package adminui

import (
	"log/slog"
	"net/http"
	"strings"
)

func (c *Console) yagoRankSearchExplanation(r *http.Request) yagorankView {
	view := yagorankView{weights: c.ranking.Profile(r.Context()).Weights}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		return view
	}
	view.explainQuery = query
	view.explainGlobal = r.URL.Query().Get("scope") == "global"
	view.searchURL = adminSearchPageURL(query, view.explainGlobal, 1)
	if c.searchExplanation == nil {
		view.explainError = "Search explanation is not available on this node."

		return view
	}
	explanation, err := c.searchExplanation.Explain(r.Context(), query, view.explainGlobal)
	if err != nil {
		slog.WarnContext(r.Context(), "admin search explanation failed", slog.Any("error", err))
		view.explainError = "Search explanation failed."

		return view
	}
	view.explanation = &explanation

	return view
}
