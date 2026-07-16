package adminauth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
)

const (
	PathAuthStylesheet            = "/admin/auth.css"
	authStylesheetCache           = "public, max-age=31536000, immutable"
	authStylesheetRevalidateCache = "public, max-age=0, must-revalidate"
	authStylesheetRejectedCache   = "private, no-store"
	authStylesheetRevisionBytes   = 12
)

var (
	authStylesheetRevision  = mustAuthStylesheetRevision(authTemplateFS)
	authStylesheetReference = PathAuthStylesheet + "?v=" + authStylesheetRevision
)

func mustAuthStylesheetRevision(assets fs.FS) string {
	content, err := fs.ReadFile(assets, "assets/auth.css")
	if err != nil {
		panic(fmt.Sprintf("build auth stylesheet reference: %v", err))
	}
	digest := sha256.Sum256(content)

	return hex.EncodeToString(digest[:authStylesheetRevisionBytes])
}

func serveAuthStylesheet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cache := authStylesheetRevalidateCache
	if r.URL.RawQuery != "" {
		if r.URL.RawQuery != "v="+authStylesheetRevision {
			w.Header().Set("Cache-Control", authStylesheetRejectedCache)
			http.NotFound(w, r)

			return
		}
		cache = authStylesheetCache
	}
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFileFS(w, r, authTemplateFS, "assets/auth.css")
}
