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
		len(item.PublishedDate) + len(item.Favicon) + len(item.Source) +
		len(item.Images)*rawContentStringHeaderBytes
	output := rawContentResultJSONBytes + rawContentJSONStringBytes(item.Title) +
		rawContentJSONStringBytes(item.URL) + rawContentJSONStringBytes(item.Content) +
		rawContentJSONStringBytes(item.PublishedDate) +
		rawContentJSONStringBytes(item.Favicon) + rawContentJSONStringBytes(item.Source)
	if item.RawContent != nil {
		retained += len(*item.RawContent)
		output += rawContentJSONStringBytes(*item.RawContent)
	}
	for _, image := range item.Images {
		retained += len(image)
		output += rawContentJSONStringBytes(image)
	}
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
	item.Source = strings.Clone(item.Source)
	if item.RawContent != nil {
		raw := strings.Clone(*item.RawContent)
		item.RawContent = &raw
	}
	if len(item.Images) > 0 {
		retainedImages := make([]string, len(item.Images))
		for index, image := range item.Images {
			retainedImages[index] = strings.Clone(image)
		}
		item.Images = retainedImages
	}
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
