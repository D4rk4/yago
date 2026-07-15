package adminauth

import (
	"mime"
	"net/http"
)

const authJSONMediaType = "application/json"

func requireAuthJSONMediaType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != authJSONMediaType {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")

		return false
	}

	return true
}
