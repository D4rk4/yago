package adminui

import (
	"fmt"
	"log/slog"
	"net/http"
)

const adminFreelistCheckpointFailedMessage = "admin storage freelist checkpoint failed"

func (c *Console) handleConfigFreelistCheckpoint(w http.ResponseWriter, r *http.Request) {
	if c.freelistCheckpointer == nil {
		http.NotFound(w, r)

		return
	}

	result, err := c.freelistCheckpointer.CheckpointFreelists(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), adminFreelistCheckpointFailedMessage,
			slog.Int("checkpointedShards", result.ShardsCheckpointed),
			slog.Int("totalShards", result.ShardsTotal),
			slog.Any("error", err),
		)
		message := "Vault checkpoint failed. Check the node log and retry."
		if result.ShardsTotal > 0 {
			message = fmt.Sprintf(
				"Vault checkpoint saved %d of %d shard freelists. Check the node log and retry.",
				result.ShardsCheckpointed,
				result.ShardsTotal,
			)
		}
		c.render(r.Context(), w, c.tpl.config, "layout", c.configPage(r, "", message))

		return
	}

	notice := fmt.Sprintf(
		"Saved freelists for %d shard(s). Vault operations have resumed.",
		result.ShardsCheckpointed,
	)
	c.render(r.Context(), w, c.tpl.config, "layout", c.configPage(r, notice, ""))
}
