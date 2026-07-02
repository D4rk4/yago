package crawlurls

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
)

const (
	yacyShortSecondLayout = "20060102150405"
	yacyShortDayLength    = len("20060102")
)

type crawlURLFeed struct {
	Version            string
	Iam                string
	Uptime             int
	MyTime             string
	Response           string
	ChannelTitle       string
	ChannelDescription string
	ChannelPubDate     string
	Items              []crawlURLItem
}

type crawlURLItem struct {
	Title       string
	Link        string
	Referrer    string
	Description string
	Author      string
	PubDate     string
	GUID        string
}

func (f crawlURLFeed) response() httpguard.RawResponse {
	return httpguard.RawResponse{ContentType: crawlURLContentType, Body: encodeCrawlURLFeed(f)}
}

func encodeCrawlURLFeed(f crawlURLFeed) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n\n")
	b.WriteString("<rss>\n\n")
	b.WriteString("<yacy version=\"")
	b.WriteString(escapeXMLAttribute(f.Version))
	b.WriteString("\">\n")
	writeXMLElement(&b, "iam", f.Iam)
	writeXMLElement(&b, "uptime", fmt.Sprintf("%d", f.Uptime))
	writeXMLElement(&b, "mytime", f.MyTime)
	writeXMLElement(&b, "response", f.Response)
	b.WriteString("</yacy>\n\n")
	b.WriteString("<channel>\n")
	writeXMLElement(&b, "title", f.ChannelTitle)
	writeXMLElement(&b, "description", f.ChannelDescription)
	writeXMLElement(&b, "pubDate", f.ChannelPubDate)
	for _, item := range f.Items {
		b.WriteString("<item>\n")
		writeXMLElement(&b, "title", item.Title)
		writeXMLElement(&b, "link", item.Link)
		writeXMLElement(&b, "referrer", item.Referrer)
		writeXMLElement(&b, "description", item.Description)
		writeXMLElement(&b, "author", item.Author)
		writeXMLElement(&b, "pubDate", item.PubDate)
		writeGUIDElement(&b, item.GUID)
		b.WriteString("</item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")

	return b.String()
}

func writeXMLElement(b *strings.Builder, key, value string) {
	b.WriteByte('<')
	b.WriteString(key)
	b.WriteByte('>')
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</")
	b.WriteString(key)
	b.WriteString(">\n")
}

func writeGUIDElement(b *strings.Builder, value string) {
	b.WriteString("<guid isPermaLink=\"false\">")
	_ = xml.EscapeText(b, []byte(value))
	b.WriteString("</guid>\n")
}

func escapeXMLAttribute(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)

	return replacer.Replace(value)
}

func remoteCrawlItems(items []RemoteCrawlURL) []crawlURLItem {
	out := make([]crawlURLItem, 0, len(items))
	for _, item := range items {
		out = append(out, crawlURLItem{
			Link:        item.Link,
			Referrer:    item.Referrer,
			Description: item.Description,
			PubDate:     formatYaCyShortSecond(item.PublishedAt),
			GUID:        item.GUID.String(),
		})
	}

	return out
}

func metadataItems(
	ctx context.Context,
	rows []yacymodel.URIMetadataRow,
	referrers map[yacymodel.Hash]string,
) ([]crawlURLItem, error) {
	items := make([]crawlURLItem, 0, len(rows))
	for _, row := range rows {
		item, err := metadataItem(ctx, row, referrers)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, nil
}

func metadataItem(
	ctx context.Context,
	row yacymodel.URIMetadataRow,
	referrers map[yacymodel.Hash]string,
) (crawlURLItem, error) {
	decoded, err := decodedURLProperties(ctx, row, []string{
		yacymodel.URLMetaColDescription,
		yacymodel.URLMetaURL,
		yacymodel.URLMetaAuthor,
	})
	if err != nil {
		return crawlURLItem{}, err
	}

	hash, err := row.URLHash()
	if err != nil {
		return crawlURLItem{}, fmt.Errorf("url metadata hash: %w", err)
	}

	referrer := ""
	if raw := row.Properties[yacymodel.URLMetaReferrer]; raw != "" {
		referrer = referrers[yacymodel.Hash(raw)]
	}

	title := decoded[yacymodel.URLMetaColDescription]

	return crawlURLItem{
		Title:       title,
		Link:        decoded[yacymodel.URLMetaURL],
		Referrer:    referrer,
		Description: title,
		Author:      decoded[yacymodel.URLMetaAuthor],
		PubDate:     metadataPubDate(row),
		GUID:        hash.String(),
	}, nil
}

func decodedURLProperties(
	ctx context.Context,
	row yacymodel.URIMetadataRow,
	keys []string,
) (map[string]string, error) {
	decoded := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := decodedURLProperty(ctx, row, key)
		if err != nil {
			return nil, err
		}
		decoded[key] = value
	}

	return decoded, nil
}

func decodedURLProperty(
	ctx context.Context,
	row yacymodel.URIMetadataRow,
	key string,
) (string, error) {
	raw := row.Properties[key]
	if raw == "" {
		return "", nil
	}

	value, err := yacymodel.DecodeWireForm(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("decode url metadata %s: %w", key, err)
	}

	return value, nil
}

func metadataPubDate(row yacymodel.URIMetadataRow) string {
	modified := row.Properties[yacymodel.ColModDate]
	if len(modified) == yacyShortDayLength {
		return modified + "000000"
	}

	return modified
}

func formatYaCyShortSecond(t time.Time) string {
	return t.UTC().Format(yacyShortSecondLayout)
}
