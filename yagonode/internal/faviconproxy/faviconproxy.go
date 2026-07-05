// Package faviconproxy serves result favicons through this node, so the hosts
// behind search results never see the searcher's browser before a click (the
// privacy property YaCy gets from its ViewImage proxy). Icons are fetched over
// the node's guarded egress client, size-bounded, sniffed to raster image types,
// and cached in memory with positive and negative TTLs; anything else answers
// with a neutral built-in placeholder so result layout stays stable.
package faviconproxy

import (
	"bytes"
	"context"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Path is the favicon endpoint; URLFor builds links to it.
const Path = "/favicon"

const (
	maxIconBytes      = 64 << 10
	positiveTTL       = 24 * time.Hour
	negativeTTL       = time.Hour
	fetchTimeout      = 5 * time.Second
	defaultFetchSlots = 8
)

// URLFor returns the proxied favicon link for a result host.
func URLFor(host string) string {
	return Path + "?host=" + template.URLQueryEscaper(host)
}

// clock feeds cache expiry; tests substitute a scripted time.
var clock = time.Now

// fetchedIcon is one fetch outcome handed to the cache; a nil body means the
// placeholder answers for this host until the entry expires.
type fetchedIcon struct {
	body        []byte
	contentType string
	expires     time.Time
}

// Proxy is the favicon endpoint handler.
type Proxy struct {
	client      *http.Client
	slots       chan struct{}
	cache       *iconCache
	placeholder []byte
}

// Mount serves GET /favicon?host=<hostname> through the given egress-guarded
// client. A nil client leaves the route unmounted.
func Mount(mux *http.ServeMux, client *http.Client) {
	if client == nil {
		return
	}
	mux.Handle("GET "+Path, New(client, defaultFetchSlots))
}

// New builds the proxy with a bounded number of concurrent origin fetches and
// the LRU icon cache (256 MiB of deduplicated icon bodies).
func New(client *http.Client, fetchSlots int) *Proxy {
	return newWithCache(client, fetchSlots, newIconCache(maxCacheBytes, maxCacheEntries))
}

func newWithCache(client *http.Client, fetchSlots int, cache *iconCache) *Proxy {
	return &Proxy{
		client:      client,
		slots:       make(chan struct{}, fetchSlots),
		cache:       cache,
		placeholder: placeholderPNG(),
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := normalizedHost(r.URL.Query().Get("host"))
	if host == "" {
		http.Error(w, "missing or invalid host parameter", http.StatusBadRequest)

		return
	}

	if body, contentType, ok := p.cache.get(host); ok {
		p.serve(w, body, contentType)

		return
	}

	icon := p.fetch(r.Context(), host)
	p.cache.put(host, icon.body, icon.contentType, icon.expires)
	p.serve(w, icon.body, icon.contentType)
}

func (p *Proxy) serve(w http.ResponseWriter, body []byte, contentType string) {
	if body == nil {
		body = p.placeholder
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	// Binary image bytes under an image content type with nosniff — the XSS
	// escaping rule below does not apply to non-HTML payloads.
	_, _ = w.Write(body) // nosemgrep
}

// fetch pulls https://<host>/favicon.ico through the guarded client. Every
// failure path yields the placeholder with the shorter negative TTL.
func (p *Proxy) fetch(ctx context.Context, host string) fetchedIcon {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		// All fetch slots busy: answer with the placeholder now instead of
		// queueing origin fetches behind a slow host, and let a later request
		// retry once the negative TTL lapses.
		return p.negative()
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	// normalizedHost admitted a plain hostname, so this URL always parses.
	// Fetching a caller-named host is this proxy's purpose; SSRF is contained by
	// the strict hostname validation (no port/path/userinfo/IP literals), the
	// egress-guarded client that refuses private networks at dial time, and the
	// size-bounded raster-only response handling below.
	req, _ := http.NewRequestWithContext( //nolint:gosec // G704: see containment above.
		fetchCtx,
		http.MethodGet,
		"https://"+host+"/favicon.ico",
		nil,
	)
	resp, err := p.client.Do(req) //nolint:gosec // G704: same containment.
	if err != nil {
		return p.negative()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return p.negative()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxIconBytes {
		return p.negative()
	}
	contentType := http.DetectContentType(body)
	if !rasterImageType(contentType) {
		return p.negative()
	}

	return fetchedIcon{
		body:        body,
		contentType: contentType,
		expires:     clock().Add(positiveTTL),
	}
}

// negative marks a host as having no usable icon: the entry carries no body, so
// it costs the cache nothing and the placeholder answers until it expires.
func (p *Proxy) negative() fetchedIcon {
	return fetchedIcon{
		contentType: "image/png",
		expires:     clock().Add(negativeTTL),
	}
}

// rasterImageType admits sniffed raster icon types only; SVG (XML) stays out so
// the proxy can never relay script-bearing markup.
func rasterImageType(contentType string) bool {
	return strings.HasPrefix(contentType, "image/") &&
		!strings.Contains(contentType, "svg")
}

// normalizedHost validates a bare lowercase hostname: no scheme, port, path,
// userinfo, or IP-literal brackets — the fetch always goes to https://<host>/.
func normalizedHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" || strings.ContainsAny(host, "/@:[] \t") {
		return ""
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Hostname() != host {
		return ""
	}

	return host
}

// placeholderPNG renders the neutral 16x16 icon served when a host has no
// usable favicon, keeping result rows visually aligned.
func placeholderPNG() []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, 16, 16))
	fill := color.RGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff}
	for y := 3; y < 13; y++ {
		for x := 3; x < 13; x++ {
			canvas.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, canvas)

	return buf.Bytes()
}
