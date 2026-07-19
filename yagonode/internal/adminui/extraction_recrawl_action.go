package adminui

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

const adminExtractionRecrawlFailureMessage = "admin extraction recrawl failed"

const extractionRecrawlPath = "/admin/index/recrawl-extraction"

func (c *Console) handleExtractionRecrawl(w http.ResponseWriter, r *http.Request) {
	if c.extractionRecrawl == nil {
		http.NotFound(w, r)

		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("limit")))
	if err != nil || limit < 1 || limit > MaximumExtractionRecrawlLimit {
		http.Error(
			w,
			"limit must be between 1 and "+strconv.Itoa(MaximumExtractionRecrawlLimit),
			http.StatusBadRequest,
		)

		return
	}
	actionID := strings.TrimSpace(r.PostFormValue("action_id"))
	if !validExtractionRecrawlActionID(actionID) {
		http.Error(w, "invalid extraction refresh action", http.StatusBadRequest)

		return
	}
	result, err := c.extractionRecrawl.QueueOutdatedExtractions(
		r.Context(),
		actionID,
		strings.TrimSpace(r.PostFormValue("continuation")),
		limit,
	)
	if err != nil {
		slog.WarnContext(
			r.Context(),
			adminExtractionRecrawlFailureMessage,
			slog.Any("error", err),
		)
		c.renderIndexPage(w, r, indexNotes{
			ExtractionRecrawl:      &result,
			ExtractionRecrawlError: "The bounded extraction refresh did not complete.",
		})

		return
	}

	c.renderIndexPage(w, r, indexNotes{ExtractionRecrawl: &result})
}
