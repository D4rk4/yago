package tavilyapi

import (
	"encoding/json"
)

func (r ExtractResult) MarshalJSON() ([]byte, error) {
	type extractResultWire struct {
		URL        string    `json:"url"`
		RawContent string    `json:"raw_content"`
		Images     *[]string `json:"images,omitempty"`
		Favicon    string    `json:"favicon,omitempty"`
	}
	var images *[]string
	if r.includeImages {
		values := r.Images
		if values == nil {
			values = []string{}
		}
		images = &values
	}

	encoded, _ := json.Marshal(extractResultWire{
		URL: r.URL, RawContent: r.RawContent, Images: images, Favicon: r.Favicon,
	})

	return encoded, nil
}

func (r CrawlResult) MarshalJSON() ([]byte, error) {
	type crawlResultWire struct {
		URL        string    `json:"url"`
		RawContent string    `json:"raw_content"`
		Images     *[]string `json:"images,omitempty"`
		Favicon    string    `json:"favicon,omitempty"`
	}
	var images *[]string
	if r.includeImages {
		values := r.Images
		if values == nil {
			values = []string{}
		}
		images = &values
	}

	encoded, _ := json.Marshal(crawlResultWire{
		URL: r.URL, RawContent: r.RawContent, Images: images, Favicon: r.Favicon,
	})

	return encoded, nil
}
