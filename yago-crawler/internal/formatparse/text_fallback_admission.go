package formatparse

import (
	"net/http"
	"strings"
)

var sniffedPlainTextExtensions = set("csv", "md", "markdown", "tex", "txt")

func bodyAllowsTextFallback(body []byte) bool {
	detected := mimeType(http.DetectContentType(body))

	return strings.HasPrefix(detected, "text/")
}
