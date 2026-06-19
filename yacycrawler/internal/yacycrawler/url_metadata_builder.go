package yacycrawler

import (
	"strconv"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const shortDayFormat = "20060102"

func BuildMetadata(page ParsedPage, loadedAt time.Time) yacymodel.URIMetadataRow {
	return buildMetadata(page, BuildPageStats(page), loadedAt)
}

func buildMetadata(
	page ParsedPage,
	stats PageStats,
	loadedAt time.Time,
) yacymodel.URIMetadataRow {
	day := loadedAt.UTC().Format(shortDayFormat)
	properties := map[string]string{
		yacymodel.URLMetaHash: string(URLHash(page.URL)),
		"url":                 yacymodel.EncodeBase64WireForm(page.URL),
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
