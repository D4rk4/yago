package adminui

import "net/http"

const (
	autocrawlerPath                  = "/admin/autocrawler"
	autocrawlerConfigurationLocation = configPath + "#panel-crawler"
)

func handleAutocrawlerRedirect(w http.ResponseWriter, r *http.Request) {
	status := http.StatusPermanentRedirect
	if r.Method != http.MethodGet {
		status = http.StatusSeeOther
	}
	http.Redirect(w, r, autocrawlerConfigurationLocation, status)
}
