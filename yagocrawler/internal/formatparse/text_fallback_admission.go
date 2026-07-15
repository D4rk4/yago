package formatparse

import (
	"net/http"
	"strings"
)

var sniffedPlainTextExtensions = set("csv", "tex", "txt")

func bodyAllowsTextFallback(body []byte) bool {
	detected := mimeType(http.DetectContentType(body))

	return strings.HasPrefix(detected, "text/")
}
