package faviconproxy

import (
	"context"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ImagePath is the page-image proxy endpoint the image vertical uses, so image
// thumbnails load through this node instead of exposing the searcher's browser
// to every pictured host.
const ImagePath = "/imgproxy"

const (
	maxImageBytes        = 512 << 10
	imageCacheBytes      = 32 << 20
	imageCacheEntries    = 4096
	imageFetchSlotCount  = 8
	maximumImageURLBytes = 8 << 10
)

// ImageURLFor returns the proxied link for a page image URL.
func ImageURLFor(rawURL string) string {
	return ImagePath + "?u=" + template.URLQueryEscaper(rawURL)
}

// ImageProxy serves full page images with the favicon proxy's containment:
// guarded egress, size bound, raster-only sniffing, LRU-cached bodies.
type ImageProxy struct {
	client      *http.Client
	slots       chan struct{}
	cache       *iconCache
	placeholder []byte
}

// MountImages serves GET /imgproxy?u=<url> through the guarded client. A nil
// client leaves the route unmounted.
func MountImages(mux *http.ServeMux, client *http.Client) {
	if client == nil {
		return
	}
	mux.Handle("GET "+ImagePath, NewImageProxy(client))
}

// NewImageProxy builds the page-image proxy with its own smaller cache, so big
// thumbnails cannot evict the favicon working set.
func NewImageProxy(client *http.Client) *ImageProxy {
	return &ImageProxy{
		client:      client,
		slots:       make(chan struct{}, imageFetchSlotCount),
		cache:       newIconCache(imageCacheBytes, imageCacheEntries),
		placeholder: placeholderPNG(),
	}
}

func (p *ImageProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := normalizedImageURL(r.URL.Query().Get("u"))
	if target == "" {
		http.Error(w, "missing or invalid u parameter", http.StatusBadRequest)

		return
	}

	if body, contentType, ok := p.cache.get(target); ok {
		serveImage(w, body, contentType, p.placeholder)

		return
	}
	icon := p.fetch(r.Context(), target)
	p.cache.put(target, icon.body, icon.contentType, icon.expires)
	serveImage(w, icon.body, icon.contentType, p.placeholder)
}

func serveImage(w http.ResponseWriter, body []byte, contentType string, placeholder []byte) {
	if body == nil {
		body = placeholder
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// Binary image bytes under an image content type with nosniff.
	_, _ = w.Write(body) // nosemgrep
}

func (p *ImageProxy) fetch(ctx context.Context, target string) fetchedIcon {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return fetchedIcon{contentType: "image/png", expires: clock().Add(negativeTTL)}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	// normalizedImageURL admitted an absolute http(s) URL, so this parses.
	// Fetching a caller-named URL is this proxy's purpose; SSRF is contained by
	// the scheme/host validation, the egress-guarded client that refuses
	// private networks at dial time, and the bounded raster-only handling.
	req, _ := http.NewRequestWithContext( //nolint:gosec // G704: contained above.
		fetchCtx,
		http.MethodGet,
		target,
		nil,
	)
	resp, err := p.client.Do(req) //nolint:gosec // G704: same containment.
	if err != nil {
		return fetchedIcon{contentType: "image/png", expires: clock().Add(negativeTTL)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fetchedIcon{contentType: "image/png", expires: clock().Add(negativeTTL)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxImageBytes {
		return fetchedIcon{contentType: "image/png", expires: clock().Add(negativeTTL)}
	}
	contentType := http.DetectContentType(body)
	if !rasterImageType(contentType) {
		return fetchedIcon{contentType: "image/png", expires: clock().Add(negativeTTL)}
	}

	return fetchedIcon{body: body, contentType: contentType, expires: clock().Add(positiveTTL)}
}

// normalizedImageURL admits absolute http(s) URLs with a hostname and no
// userinfo; anything else is rejected before a fetch is attempted.
func normalizedImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > maximumImageURLBytes {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	hostname := strings.ToLower(parsed.Hostname())
	if net.ParseIP(hostname) == nil && !validDNSHost(hostname) {
		return ""
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	port := parsed.Port()
	switch {
	case port != "":
		parsed.Host = net.JoinHostPort(hostname, port)
	case strings.Contains(hostname, ":"):
		parsed.Host = "[" + hostname + "]"
	default:
		parsed.Host = hostname
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	normalized := parsed.String()
	if len(normalized) > maximumImageURLBytes {
		return ""
	}

	return normalized
}
