package adminui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/httproute"
)

const (
	adminAssetRevisionBytes            = 12
	adminAssetPath                     = "/admin/assets/"
	adminAssetImmutableCacheControl    = "public, max-age=31536000, immutable"
	adminAssetRevalidationCacheControl = "no-cache"
	adminAssetRejectedCacheControl     = "no-store"
)

type adminAssetRecord struct {
	reference string
	revision  string
}

type adminAssetCatalog map[string]adminAssetRecord

var embeddedAdminAssetCatalog = mustAdminAssetCatalog(assetFS)

func adminAssetTemplateFunctions() template.FuncMap {
	return template.FuncMap{
		"asset": func(name string) (string, error) {
			asset, ok := embeddedAdminAssetCatalog[name]
			if !ok {
				return "", fmt.Errorf("admin asset %q is not embedded", name)
			}
			return asset.reference, nil
		},
	}
}

func mustAdminAssetReferences(assets fs.FS) map[string]string {
	catalog := mustAdminAssetCatalog(assets)

	return adminAssetReferences(catalog)
}

func buildAdminAssetReferences(assets fs.FS) (map[string]string, error) {
	catalog, err := buildAdminAssetCatalog(assets)
	if err != nil {
		return nil, err
	}

	return adminAssetReferences(catalog), nil
}

func mustAdminAssetCatalog(assets fs.FS) adminAssetCatalog {
	catalog, err := buildAdminAssetCatalog(assets)
	if err != nil {
		panic(fmt.Sprintf("build admin asset catalog: %v", err))
	}

	return catalog
}

func buildAdminAssetCatalog(assets fs.FS) (adminAssetCatalog, error) {
	catalog := make(adminAssetCatalog)
	err := fs.WalkDir(assets, "assets", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		content, err := fs.ReadFile(assets, name)
		if err != nil {
			return fmt.Errorf("read admin asset %q: %w", name, err)
		}
		digest := sha256.Sum256(content)
		revision := hex.EncodeToString(digest[:adminAssetRevisionBytes])
		assetName := strings.TrimPrefix(name, "assets/")
		catalog[assetName] = adminAssetRecord{
			reference: adminAssetPath + assetName + "?v=" + revision,
			revision:  revision,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk admin assets: %w", err)
	}

	return catalog, nil
}

func adminAssetReferences(catalog adminAssetCatalog) map[string]string {
	references := make(map[string]string, len(catalog))
	for name, asset := range catalog {
		references[name] = asset.reference
	}

	return references
}

func RejectAdminAssetAliases(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rejectAdminAssetAlias(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func rejectAdminAssetAlias(w http.ResponseWriter, r *http.Request) bool {
	if !isAdminAssetTarget(r) {
		return false
	}
	if _, canonical := canonicalAdminAssetName(r); canonical {
		return false
	}
	writeRejectedAdminAsset(w, r)

	return true
}

func isAdminAssetTarget(r *http.Request) bool {
	assetRoot := strings.TrimSuffix(adminAssetPath, "/")
	cleaned := httproute.CanonicalPath(r.URL.Path)

	return r.URL.Path == assetRoot || strings.HasPrefix(r.URL.Path, adminAssetPath) ||
		cleaned == assetRoot || strings.HasPrefix(cleaned, adminAssetPath)
}

func canonicalAdminAssetName(r *http.Request) (string, bool) {
	if r.URL.RawPath != "" || r.URL.EscapedPath() != r.URL.Path ||
		!strings.HasPrefix(r.URL.Path, adminAssetPath) {
		return "", false
	}
	name := strings.TrimPrefix(r.URL.Path, adminAssetPath)
	if name == "" || !fs.ValidPath(name) {
		return "", false
	}

	return name, true
}

func adminAssetCacheControl(rawQuery, revision string) (string, bool) {
	if rawQuery == "" {
		return adminAssetRevalidationCacheControl, true
	}
	if rawQuery != "v="+revision {
		return "", false
	}

	return adminAssetImmutableCacheControl, true
}

func writeRejectedAdminAsset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", adminAssetRejectedCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.NotFound(w, r)
}

type adminAssetResponseWriter struct {
	http.ResponseWriter
}

func (w adminAssetResponseWriter) WriteHeader(status int) {
	if status >= http.StatusBadRequest ||
		status >= http.StatusMultipleChoices && status != http.StatusNotModified {
		w.Header().Set("Cache-Control", adminAssetRejectedCacheControl)
	}
	w.ResponseWriter.WriteHeader(status)
}
