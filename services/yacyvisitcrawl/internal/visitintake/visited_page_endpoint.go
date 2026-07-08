package visitintake

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const (
	queryParamURL      = "url"
	msgVisitRejected   = "visit rejected"
	msgVisitRedirected = "visit redirected"
)

type visitedPageEndpoint struct {
	placement    CrawlOrderPlacement
	profile      yacycrawlcontract.CrawlProfile
	metrics      VisitMetrics
	maxBodyBytes int64
}

func (e visitedPageEndpoint) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, e.maxBodyBytes)

	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	e.metrics.VisitReceived()

	visitedPage, err := parseVisitedPage(req.URL.Query().Get(queryParamURL))
	if err != nil {
		e.metrics.VisitRejected()
		slog.WarnContext(req.Context(), msgVisitRejected, slog.Any("error", err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	e.placement.Attempt(crawlOrderFromVisit(visitedPage, e.profile))

	slog.DebugContext(req.Context(), msgVisitRedirected, slog.String("visitedPage", visitedPage))
	//nolint:gosec // G710: redirecting to the visited page is this endpoint's purpose; parseVisitedPage already restricts scheme and requires a host.
	http.Redirect(w, req, visitedPage, http.StatusFound)
}

func parseVisitedPage(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%s: must be set", queryParamURL)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", queryParamURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s: scheme must be http or https", queryParamURL)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s: must include a host", queryParamURL)
	}
	return parsed.String(), nil
}
