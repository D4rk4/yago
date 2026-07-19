package adminui

import (
	"log/slog"
	"net/http"
	"strings"
)

const adminDocumentDetailFailureMessage = "admin document detail lookup failed"

type indexDocumentPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Document   DocumentDetail
}

func (c *Console) handleIndexDocument(w http.ResponseWriter, r *http.Request) {
	if c.documentDetail == nil {
		http.NotFound(w, r)

		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("url"))
	if key == "" {
		http.Error(w, "document URL is required", http.StatusBadRequest)

		return
	}
	document, found, err := c.documentDetail.DocumentDetail(r.Context(), key)
	if err != nil {
		slog.WarnContext(r.Context(), adminDocumentDetailFailureMessage, slog.Any("error", err))
		c.renderUnavailable(
			w,
			r,
			indexPath,
			"Document detail",
			"Document detail is not available.",
		)

		return
	}
	if !found {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.indexDocument, "layout", indexDocumentPageData{
		AppName: appName, ActivePath: indexPath, Nav: navItems,
		CSRF:     csrfToken(r),
		Section:  sectionView{Heading: "Document detail", Available: true},
		Document: document,
	})
}
