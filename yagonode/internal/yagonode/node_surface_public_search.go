package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/portaltheme"
	"github.com/D4rk4/yago/yagonode/internal/rankingprofile"
	"github.com/D4rk4/yago/yagonode/internal/searchactivity"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tavilyapi"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type surfacePublicSearchInput struct {
	mux       *http.ServeMux
	assembly  assembleSurfacesInput
	runtime   crawlProcess
	ranking   *rankingprofile.Holder
	denylist  *urldenylist.Store
	activity  *searchactivity.Tracker
	signals   corpusSignalSet
	theme     *portaltheme.Theme
	learning  searchLearningStores
	admission tavilyapi.SearchAdmission
}

func mountSurfacePublicSearch(
	input surfacePublicSearchInput,
) (searchcore.Searcher, searchcore.Searcher, searchcore.Searcher) {
	parts := publicSearchParts{
		runtime:    input.runtime,
		ranking:    input.ranking,
		denylist:   input.denylist,
		activity:   input.activity,
		hostRank:   input.signals.authority,
		spell:      input.signals.spelling,
		words:      input.signals.wordForms,
		theme:      input.theme,
		clicks:     input.learning.clicks,
		models:     input.learning.models,
		reputation: input.learning.reputation,
		peerEvents: input.learning.peerEvents,
		admission:  input.admission,
	}

	return mountNodePublicSearchWithExplanation(
		input.mux,
		newPublicSearchAssembly(input.assembly, parts),
	)
}
