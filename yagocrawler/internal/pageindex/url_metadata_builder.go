package pageindex

import (
	"strconv"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pageparse"
	"github.com/D4rk4/yago/yagomodel"
)

const shortDayFormat = "20060102"

func BuildMetadata(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
	loadedAt time.Time,
) yagomodel.URIMetadataRow {
	day := loadedAt.UTC().Format(shortDayFormat)
	urlHash, _ := yagomodel.HashURL(page.URL)
	properties := map[string]string{
		yagomodel.URLMetaHash: urlHash.String(),
		yagomodel.URLMetaURL:  yagomodel.EncodeBase64WireForm(page.URL),
		"descr":               yagomodel.EncodeBase64WireForm(page.Title),
		"dt":                  "t",
		"lang":                NormalizeLanguage(page.Language),
		"mod":                 day,
		"load":                day,
		"fresh":               day,
		"size":                strconv.Itoa(len(page.Text)),
		"wc":                  strconv.Itoa(len(stats.Tokens)),
		"llocal":              strconv.Itoa(len(stats.LocalLinks)),
		"lother":              strconv.Itoa(len(stats.ExternalLinks)),
	}
	return yagomodel.URIMetadataRow{Properties: properties}
}
