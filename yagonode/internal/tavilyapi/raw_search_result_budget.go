package tavilyapi

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func newRawSearchResultBudget(limit int) *rawContentBudget {
	return &rawContentBudget{
		retained: rawContentEnvelopeBytes + limit*rawContentSearchResultBytes +
			maxResponseImages*rawContentSearchImageBytes,
		output: rawContentEnvelopeBytes,
	}
}

func retainRawSearchResult(
	budget *rawContentBudget,
	item SearchResult,
	images []SearchImage,
) (SearchResult, []SearchImage, error) {
	retained := len(item.Title) + len(item.URL) + len(item.Content) +
		len(item.PublishedDate) + len(item.Favicon)
	output := rawContentResultJSONBytes + rawContentJSONStringBytes(item.Title) +
		rawContentJSONStringBytes(item.URL) + rawContentJSONStringBytes(item.Content) +
		rawContentJSONStringBytes(item.PublishedDate) +
		rawContentJSONStringBytes(item.Favicon)
	if item.RawContent != nil {
		retained += len(*item.RawContent)
		output += rawContentJSONStringBytes(*item.RawContent)
	}
	imageRetained, imageOutput := retainedSearchResultImages(item.Images)
	retained += imageRetained
	output += imageOutput
	for _, image := range images {
		retained += len(image.URL) + len(image.Description)
		output += rawContentJSONStringBytes(image.URL) +
			rawContentJSONStringBytes(image.Description)
	}
	if !budget.reserve(retained, output) {
		return SearchResult{}, nil, errRawContentBudgetExceeded
	}
	item.Title = strings.Clone(item.Title)
	item.URL = strings.Clone(item.URL)
	item.Content = strings.Clone(item.Content)
	item.PublishedDate = strings.Clone(item.PublishedDate)
	item.Favicon = strings.Clone(item.Favicon)
	if item.RawContent != nil {
		raw := strings.Clone(*item.RawContent)
		item.RawContent = &raw
	}
	item.Images = cloneSearchResultImages(item.Images)
	retainedImages := make([]SearchImage, len(images))
	for index, image := range images {
		retainedImages[index] = SearchImage{
			URL: strings.Clone(image.URL), Description: strings.Clone(image.Description),
		}
	}

	return item, retainedImages, nil
}

func rawDocumentResultContent(
	req SearchRequest,
	doc documentstore.Document,
) (*string, error) {
	if !req.IncludeRawContent.Enabled() || doc.ExtractedText == "" {
		return nil, nil
	}
	raw := doc.ExtractedText
	if strings.EqualFold(string(req.IncludeRawContent), "markdown") {
		bounded, ok := boundedDocumentMarkdown(doc, maximumRawContentResponseBytes)
		if !ok {
			return nil, errRawContentBudgetExceeded
		}
		raw = bounded
	}

	return &raw, nil
}
