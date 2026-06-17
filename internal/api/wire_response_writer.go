package api

import (
	"io"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const wireContentType = "text/plain; charset=UTF-8"

func writeWireMessage(w http.ResponseWriter, msg yacymodel.Message) {
	w.Header().Set("Content-Type", wireContentType)
	_, _ = io.WriteString(w, msg.Encode())
}
