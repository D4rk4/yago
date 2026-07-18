package tavilyapi

import (
	"encoding/json"
	"net/http"
)

type crawlSuccessResponse struct {
	request   CrawlRequest
	pages     []CrawlResult
	baseURL   string
	elapsed   float64
	requestID string
}

func (e crawlEndpoint) writeCrawlSuccess(
	w http.ResponseWriter,
	response crawlSuccessResponse,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if e.mapOnly {
		_ = json.NewEncoder(w).Encode(MapResponse{
			BaseURL: response.baseURL, Results: pageURLs(response.pages),
			ResponseTime: response.elapsed,
			Usage:        responseUsageEnabled(response.request.IncludeUsage),
			RequestID:    response.requestID,
		})

		return
	}
	_ = json.NewEncoder(w).Encode(CrawlResponse{
		BaseURL: response.baseURL, Results: response.pages, ResponseTime: response.elapsed,
		Usage: responseUsageEnabled(response.request.IncludeUsage), RequestID: response.requestID,
	})
}
