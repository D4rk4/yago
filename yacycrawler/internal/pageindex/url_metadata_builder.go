package pageindex

import (
	"fmt"
	"strconv"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const shortDayFormat = "20060102"

func BuildMetadata(
	page crawlwork.ParsedPage,
	stats crawlwork.PageStats,
	loadedAt time.Time,
) (yacymodel.URIMetadataRow, error) {
	day := loadedAt.UTC().Format(shortDayFormat)
	urlHash, err := yacymodel.HashURL(page.URL)
	if err != nil {
		return yacymodel.URIMetadataRow{}, fmt.Errorf("hash url: %w", err)
	}
	properties := map[string]string{
		yacymodel.URLMetaHash: urlHash.String(),
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
	return yacymodel.URIMetadataRow{Properties: properties}, nil
}
