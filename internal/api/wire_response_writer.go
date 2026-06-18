package api

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const wireContentType = "text/plain; charset=UTF-8"

func writeWireMessage(w http.ResponseWriter, msg yacymodel.Message) {
	w.Header().Set("Content-Type", wireContentType)
	if _, err := io.WriteString(w, msg.Encode()); err != nil {
		slog.Warn("wire response write failed", "error", err)
	}
}
