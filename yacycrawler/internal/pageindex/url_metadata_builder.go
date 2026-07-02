package pageindex

import (
	"strconv"
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/pageparse"
	"github.com/D4rk4/yago/yacymodel"
)

const shortDayFormat = "20060102"

func BuildMetadata(
	page pageparse.ParsedPage,
	stats pageparse.PageStats,
	loadedAt time.Time,
) yacymodel.URIMetadataRow {
	day := loadedAt.UTC().Format(shortDayFormat)
	urlHash, _ := yacymodel.HashURL(page.URL)
	properties := map[string]string{
		yacymodel.URLMetaHash: urlHash.String(),
		yacymodel.URLMetaURL:  yacymodel.EncodeBase64WireForm(page.URL),
		"descr":               yacymodel.EncodeBase64WireForm(page.Title),
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
	return yacymodel.URIMetadataRow{Properties: properties}
}
