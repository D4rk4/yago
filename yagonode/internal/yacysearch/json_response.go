package yacysearch

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type jsonResponse struct {
	Channels []jsonChannel `json:"channels"`
}

type jsonChannel struct {
	Title           string                      `json:"title"`
	Description     string                      `json:"description"`
	Link            string                      `json:"link"`
	Image           jsonImage                   `json:"image"`
	StartIndex      string                      `json:"startIndex"`
	ItemsPerPage    string                      `json:"itemsPerPage"`
	SearchTerms     string                      `json:"searchTerms"`
	Items           []jsonItem                  `json:"items"`
	Navigation      []jsonNavigation            `json:"navigation"`
	TotalResults    string                      `json:"totalResults"`
	PartialFailures []searchcore.PartialFailure `json:"partialFailures,omitempty"`
}

type jsonImage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Link  string `json:"link"`
}

type jsonItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Code        string `json:"code"`
	Description string `json:"description"`
	PubDate     string `json:"pubDate"`
	Size        string `json:"size"`
	SizeName    string `json:"sizename"`
	GUID        string `json:"guid"`
	Host        string `json:"host"`
	Path        string `json:"path"`
	File        string `json:"file"`
	URLHash     string `json:"urlhash"`
	Ranking     string `json:"ranking"`
}

type jsonNavigation struct {
	FacetName   string              `json:"facetname"`
	DisplayName string              `json:"displayname"`
	Type        string              `json:"type"`
	Min         string              `json:"min"`
	Max         string              `json:"max"`
	Mean        string              `json:"mean"`
	Elements    []map[string]string `json:"elements"`
}

func responseJSON(r *http.Request, resp searchcore.Response) jsonResponse {
	base := searchBaseURL(r)
	link := searchLink(base, resp.Request)

	return jsonResponse{Channels: []jsonChannel{
		{
			Title:       "YaCy P2P-Search for " + resp.Request.Query,
			Description: "Search for " + resp.Request.Query,
			Link:        link,
			Image: jsonImage{
				URL:   searchImageURL(r),
				Title: "Search for " + resp.Request.Query,
				Link:  link,
			},
			StartIndex:      strconv.Itoa(resp.Request.Offset),
			ItemsPerPage:    strconv.Itoa(resp.Request.Limit),
			SearchTerms:     url.QueryEscape(resp.Request.Query),
			Items:           responseItems(resp.Results),
			Navigation:      []jsonNavigation{},
			TotalResults:    strconv.Itoa(resp.TotalResults),
			PartialFailures: resp.PartialFailures,
		},
	}}
}

func responseItems(results []searchcore.Result) []jsonItem {
	items := make([]jsonItem, 0, len(results))
	for _, result := range results {
		items = append(items, jsonItem{
			Title:       markWebResultTitle(result.Source, result.Title),
			Link:        result.URL,
			Code:        result.URLHash,
			Description: result.Snippet,
			PubDate:     result.Date,
			Size:        strconv.Itoa(result.Size),
			SizeName:    sizeName(result.Size),
			GUID:        result.URLHash,
			Host:        result.Host,
			Path:        result.Path,
			File:        result.File,
			URLHash:     result.URLHash,
			Ranking:     strconv.FormatFloat(result.Score, 'f', 0, 64),
		})
	}

	return items
}

func searchLink(base string, req searchcore.Request) string {
	return htmlEscapedURL(searchURL(base, req))
}

func searchURL(base string, req searchcore.Request) string {
	return base + "?query=" + url.QueryEscape(req.Query) +
		"&resource=" + url.QueryEscape(string(req.Source)) +
		"&contentdom=" + url.QueryEscape(string(req.ContentDomain))
}

func htmlEscapedURL(raw string) string {
	return strings.ReplaceAll(raw, "&", "&amp;")
}

func searchBaseURL(r *http.Request) string {
	return requestBaseURL(r) + "/yacysearch.html"
}

func searchImageURL(r *http.Request) string {
	return requestBaseURL(r) + "/env/grafics/yacy.png"
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}

	return scheme + "://" + host
}

func sizeName(size int) string {
	return strconv.Itoa(size) + " bytes"
}
