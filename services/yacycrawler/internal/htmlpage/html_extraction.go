package htmlpage

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/canonicalurl"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const (
	mediaHTML  = "text/html"
	mediaXHTML = "application/xhtml+xml"
)

type HTMLExtraction struct{}

func New() HTMLExtraction {
	return HTMLExtraction{}
}

func (HTMLExtraction) MediaTypes() []string {
	return []string{mediaHTML, mediaXHTML}
}

func (HTMLExtraction) Extract(
	pageURL, contentType string,
	body []byte,
) ([]crawlcapability.ExtractedContent, error) {
	decoded, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, fmt.Errorf("decode charset: %w", err)
	}
	root, err := html.Parse(decoded)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	parsedURL, _ := url.Parse(pageURL)
	scan := scanTree(root)

	article, err := readability.FromDocument(root, parsedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", crawlcapability.ErrUnextractable, err)
	}
	var text bytes.Buffer
	if err := article.RenderText(&text); err != nil {
		return nil, fmt.Errorf("%w: %w", crawlcapability.ErrUnextractable, err)
	}
	if strings.TrimSpace(text.String()) == "" {
		return nil, fmt.Errorf("%w: empty content", crawlcapability.ErrUnextractable)
	}

	base := pageURL
	if scan.baseHref != "" {
		if resolved, resolveErr := resolveBase(pageURL, scan.baseHref); resolveErr == nil {
			base = resolved
		}
	}
	links, local, external := resolveLinks(base, scan.hrefs)

	return []crawlcapability.ExtractedContent{{
		Title:                article.Title(),
		Text:                 strings.TrimSpace(text.String()),
		Language:             twoLetterLanguage(article.Language()),
		Links:                links,
		LocalLinkCount:       local,
		ExternalLinkCount:    external,
		RefusesIndexing:      scan.noIndex,
		RefusesLinkDiscovery: scan.noFollow,
	}}, nil
}

func resolveBase(pageURL, baseHref string) (string, error) {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse page url: %w", err)
	}
	ref, err := url.Parse(baseHref)
	if err != nil {
		return "", fmt.Errorf("parse base href: %w", err)
	}
	return parsed.ResolveReference(ref).String(), nil
}

func resolveLinks(base string, hrefs []string) (links []string, local, external int) {
	baseHost := hostOf(base)
	seen := map[string]struct{}{}
	for _, href := range hrefs {
		canonical, err := canonicalurl.ResolveReference(base, href)
		if err != nil {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		links = append(links, canonical)
		if hostOf(canonical) == baseHost {
			local++
		} else {
			external++
		}
	}
	return links, local, external
}

func hostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func twoLetterLanguage(language string) string {
	primary := strings.ToLower(strings.TrimSpace(language))
	if dash := strings.IndexByte(primary, '-'); dash >= 0 {
		primary = primary[:dash]
	}
	if len(primary) != 2 {
		return ""
	}
	for _, r := range primary {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return primary
}
