package pageparse

import (
	"bytes"
	"mime"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

const (
	minimumStaticContentTerms = 4
	minimumStaticContentRunes = 16
)

func BrowserRenderNeeded(contentType string, body []byte) bool {
	if !browserRenderableHTML(contentType) || !hasExecutableScript(body) {
		return false
	}
	text, err := extractMainContent(contentType, body)
	if err != nil {
		return true
	}

	return !staticContentUsable(text)
}

func browserRenderableHTML(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, _, _ = strings.Cut(contentType, ";")
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}

func hasExecutableScript(body []byte) bool {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	inlineExecutable := false
	for {
		switch tokenType := tokenizer.Next(); tokenType {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			element := tokenizer.Token()
			if !strings.EqualFold(element.Data, "script") {
				continue
			}
			executable := executableScriptType(scriptAttribute(element.Attr, "type"))
			if executable && strings.TrimSpace(scriptAttribute(element.Attr, "src")) != "" {
				return true
			}
			inlineExecutable = executable && tokenType == html.StartTagToken
		case html.TextToken:
			if inlineExecutable && strings.TrimSpace(string(tokenizer.Text())) != "" {
				return true
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if strings.EqualFold(string(name), "script") {
				inlineExecutable = false
			}
		}
	}
}

func scriptAttribute(attributes []html.Attribute, name string) string {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}

	return ""
}

func executableScriptType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "module" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil {
		value = mediaType
	}

	return strings.Contains(value, "javascript") || strings.Contains(value, "ecmascript")
}

func staticContentUsable(text string) bool {
	if len(tokenize(text, minimumStaticContentTerms)) >= minimumStaticContentTerms {
		return true
	}
	meaningfulRunes := 0
	for _, character := range text {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			meaningfulRunes++
			if meaningfulRunes >= minimumStaticContentRunes {
				return true
			}
		}
	}

	return false
}
