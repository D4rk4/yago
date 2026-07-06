package contentextraction

import (
	"errors"
	"fmt"
	"mime"
	"strings"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

type MediaTypeRouter struct {
	extractors   map[string]crawlcapability.ContentExtraction
	containers   map[string]crawlcapability.ArchiveExpansion
	maxDepth     int
	maxDocuments int
}

func New(maxDepth, maxDocuments int) *MediaTypeRouter {
	return &MediaTypeRouter{
		extractors:   map[string]crawlcapability.ContentExtraction{},
		containers:   map[string]crawlcapability.ArchiveExpansion{},
		maxDepth:     maxDepth,
		maxDocuments: maxDocuments,
	}
}

func (r *MediaTypeRouter) RegisterExtractor(
	mediaType string,
	extractor crawlcapability.ContentExtraction,
) {
	r.extractors[mediaType] = extractor
}

func (r *MediaTypeRouter) RegisterContainer(
	mediaType string,
	container crawlcapability.ArchiveExpansion,
) {
	r.containers[mediaType] = container
}

func (r *MediaTypeRouter) RegisteredMediaTypes() int {
	return len(r.extractors) + len(r.containers)
}

func (r *MediaTypeRouter) Extract(
	pageURL, contentType string,
	body []byte,
) ([]crawlcapability.ExtractedDocument, error) {
	documents, err := r.route(0, pageURL, contentType, body)
	if err != nil {
		return nil, err
	}
	return documents, nil
}

func (r *MediaTypeRouter) route(
	depth int,
	resourceURL, contentType string,
	body []byte,
) ([]crawlcapability.ExtractedDocument, error) {
	media := mediaType(contentType)

	if extractor, ok := r.extractors[media]; ok {
		contents, err := extractor.Extract(resourceURL, contentType, body)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", media, err)
		}
		documents := make([]crawlcapability.ExtractedDocument, len(contents))
		for i, content := range contents {
			documents[i] = crawlcapability.ExtractedDocument{
				URL:              resourceURL,
				ExtractedContent: content,
			}
		}
		return documents, nil
	}

	container, ok := r.containers[media]
	if !ok {
		return nil, crawlcapability.ErrUnsupportedMediaType
	}
	if depth >= r.maxDepth {
		return nil, crawlcapability.ErrContainerOverflow
	}

	members, err := container.Expand(resourceURL, contentType, body)
	if err != nil {
		return nil, fmt.Errorf("expand %s: %w", media, err)
	}

	var documents []crawlcapability.ExtractedDocument
	for _, member := range members {
		fromMember, err := r.route(depth+1, member.URL, member.ContentType, member.Body)
		if err != nil {
			if errors.Is(err, crawlcapability.ErrContainerOverflow) {
				return nil, err
			}
			continue
		}
		if len(documents)+len(fromMember) > r.maxDocuments {
			return nil, crawlcapability.ErrContainerOverflow
		}
		documents = append(documents, fromMember...)
	}
	return documents, nil
}

func mediaType(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	return media
}
