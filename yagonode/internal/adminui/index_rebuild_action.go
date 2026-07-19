package adminui

import (
	"log/slog"
	"net/http"
)

const adminIndexRebuildFailureMessage = "admin index rebuild scheduling failed"

func (c *Console) handleIndexRebuild(w http.ResponseWriter, r *http.Request) {
	if c.indexRebuild == nil {
		http.NotFound(w, r)

		return
	}
	if err := c.indexRebuild.ScheduleRebuild(r.Context()); err != nil {
		slog.WarnContext(r.Context(), adminIndexRebuildFailureMessage, slog.Any("error", err))
		c.renderIndexPage(w, r, indexNotes{RebuildError: "Could not schedule the rebuild."})

		return
	}

	c.renderIndexPage(w, r, indexNotes{
		RebuildNotice: "Rebuild request accepted. The active index remains available until restart.",
	})
}
