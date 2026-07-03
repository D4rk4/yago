package sitemap

import "strings"

const robotsSitemapField = "sitemap:"

func ParseRobotsSitemaps(raw []byte, limit int) []string {
	var urls []string
	for _, line := range strings.Split(string(raw), "\n") {
		value, ok := robotsSitemapValue(line)
		if !ok {
			continue
		}
		if reachedLimit(len(urls), limit) {
			break
		}
		urls = append(urls, value)
	}
	return urls
}

func robotsSitemapValue(line string) (string, bool) {
	content := line
	if idx := strings.IndexByte(content, '#'); idx >= 0 {
		content = content[:idx]
	}
	content = strings.TrimSpace(content)
	if len(content) < len(robotsSitemapField) {
		return "", false
	}
	if !strings.EqualFold(content[:len(robotsSitemapField)], robotsSitemapField) {
		return "", false
	}
	value := strings.TrimSpace(content[len(robotsSitemapField):])
	if value == "" {
		return "", false
	}
	return value, true
}
