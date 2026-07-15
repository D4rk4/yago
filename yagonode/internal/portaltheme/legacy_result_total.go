package portaltheme

import "strings"

const legacyResultTotalFragment = `{{formatNumber results.totalResults}} {{pluralize results.totalResults "result" "results"}} for`

const currentResultTotalFragment = `{{#if results.totalResults}}Up to {{formatNumber results.totalResults}} {{pluralize results.totalResults "result" "results"}} available in this search window{{else}}0 results{{/if}} for`

func repairLegacyResultTotal(body string) string {
	return strings.ReplaceAll(body, legacyResultTotalFragment, currentResultTotalFragment)
}
