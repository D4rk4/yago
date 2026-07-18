package extractfetch

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const maximumPageImages = 20

func collectImages(doc *html.Node, rawURL string) []string {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	images := make([]string, 0, maximumPageImages)
	seen := make(map[string]bool, maximumPageImages)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if len(images) >= maximumPageImages {
			return
		}
		if node.Type == html.ElementNode && node.Data == "img" {
			if image := imageSource(node, base); image != "" && !seen[image] {
				seen[image] = true
				images = append(images, image)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return images
}

func imageSource(node *html.Node, base *url.URL) string {
	for _, attr := range node.Attr {
		if attr.Key != "src" {
			continue
		}
		resolved, err := base.Parse(strings.TrimSpace(attr.Val))
		if err != nil || resolved.Scheme != "http" && resolved.Scheme != "https" {
			return ""
		}
		resolved.Fragment = ""

		return resolved.String()
	}

	return ""
}
