package searchremote

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagoproto"
)

type groupedMorphologyFixture struct {
	tb                testing.TB
	alpha             yagomodel.Hash
	alphaSibling      yagomodel.Hash
	beta              yagomodel.Hash
	betaSibling       yagomodel.Hash
	exactDocument     yagomodel.Hash
	sharedDocument    yagomodel.Hash
	alphaOnlyDocument yagomodel.Hash
	betaOnlyDocument  yagomodel.Hash
	requestsMu        sync.Mutex
	requests          []url.Values
}

func newGroupedMorphologyFixture(tb testing.TB) *groupedMorphologyFixture {
	return &groupedMorphologyFixture{
		tb:                tb,
		alpha:             yagomodel.WordHash("alpha"),
		alphaSibling:      yagomodel.WordHash("alphas"),
		beta:              yagomodel.WordHash("beta"),
		betaSibling:       yagomodel.WordHash("betas"),
		exactDocument:     hashFor("exact-doc"),
		sharedDocument:    hashFor("shared-doc"),
		alphaOnlyDocument: hashFor("alpha-only"),
		betaOnlyDocument:  hashFor("beta-only"),
		requests:          make([]url.Values, 0, 9),
	}
}

func (f *groupedMorphologyFixture) serve(w http.ResponseWriter, r *http.Request) {
	form := r.URL.Query()
	f.requestsMu.Lock()
	f.requests = append(f.requests, form)
	f.requestsMu.Unlock()

	if abstract := form.Get(yagoproto.FieldAbstracts); abstract != "" {
		f.writeAbstract(w, yagomodel.Hash(abstract))
		return
	}
	if urls := form.Get(yagoproto.FieldURLs); urls != "" {
		f.writeMetadata(w, form.Get(yagoproto.FieldQuery), urls)
		return
	}
	f.writePrimary(w, form.Get(yagoproto.FieldQuery))
}

func (f *groupedMorphologyFixture) writeAbstract(w http.ResponseWriter, term yagomodel.Hash) {
	var documents []yagomodel.Hash
	switch term {
	case f.alpha:
		documents = []yagomodel.Hash{f.alphaOnlyDocument}
	case f.alphaSibling:
		documents = []yagomodel.Hash{f.sharedDocument, f.alphaOnlyDocument}
	case f.beta:
		documents = []yagomodel.Hash{f.sharedDocument}
	case f.betaSibling:
		documents = []yagomodel.Hash{f.betaOnlyDocument}
	default:
		f.tb.Errorf("unexpected abstract term %q", term)
	}
	writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{
		IndexCount: map[yagomodel.Hash]int{term: len(documents)},
		IndexAbstract: map[yagomodel.Hash]string{
			term: yagomodel.EncodeSearchIndexAbstract(documents),
		},
	}.Encode().Encode())
}

func (f *groupedMorphologyFixture) writeMetadata(
	w http.ResponseWriter,
	query string,
	urls string,
) {
	if urls != f.sharedDocument.String() {
		f.tb.Errorf("secondary URL set = %q, want only %q", urls, f.sharedDocument)
	}
	if query != f.alphaSibling.String() && query != f.beta.String() {
		writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{}.Encode().Encode())
		return
	}
	writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{
		JoinCount: 1,
		Count:     1,
		Resources: []yagomodel.URIMetadataRow{metadataRow(
			f.tb,
			f.sharedDocument,
			"https://example.org/grouped",
			"Alpha beta grouped result",
		)},
	}.Encode().Encode())
}

func (f *groupedMorphologyFixture) writePrimary(w http.ResponseWriter, query string) {
	if query != f.alpha.String()+f.beta.String() {
		f.tb.Errorf("primary query = %q, want exact conjunction", query)
	}
	writeFixtureResponse(f.tb, w, yagoproto.SearchResponse{
		JoinCount: 1,
		Count:     1,
		Resources: []yagomodel.URIMetadataRow{metadataRow(
			f.tb,
			f.exactDocument,
			"https://example.org/exact",
			"Alpha beta exact result",
		)},
	}.Encode().Encode())
}

func (f *groupedMorphologyFixture) recordedRequests() []url.Values {
	f.requestsMu.Lock()
	defer f.requestsMu.Unlock()

	return append([]url.Values(nil), f.requests...)
}

func TestGroupedMorphologyFindsSiblingFormWithoutRelaxingOtherRequirements(t *testing.T) {
	fixture := newGroupedMorphologyFixture(t)
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()

	response, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yagomodel.Seed{
			serverSeed(t, server.URL),
		}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 1,
		ExpandWord: func(word string) []string {
			return []string{word, word + "s"}
		},
	}).Search(t.Context(), searchcore.Request{
		Query:         "alpha beta",
		Terms:         []string{"alpha", "beta"},
		Source:        searchcore.SourceGlobal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 2 {
		t.Fatalf(
			"results = %#v failures=%#v requests=%#v",
			response.Results,
			response.PartialFailures,
			fixture.recordedRequests(),
		)
	}
	identities := map[string]bool{}
	for _, result := range response.Results {
		identities[result.URLHash] = true
	}
	if !identities[fixture.exactDocument.String()] || !identities[fixture.sharedDocument.String()] {
		t.Fatalf("result identities = %#v", identities)
	}
	if identities[fixture.alphaOnlyDocument.String()] ||
		identities[fixture.betaOnlyDocument.String()] {
		t.Fatalf("one-requirement result admitted: %#v", identities)
	}
	if !slices.Equal(response.Request.Terms, []string{"alpha", "beta"}) {
		t.Fatalf("response request terms = %v", response.Request.Terms)
	}

	recorded := fixture.recordedRequests()
	if len(recorded) != 6 {
		t.Fatalf("requests = %d, want 1 exact + 4 abstract + 1 metadata", len(recorded))
	}
	primaryRequests := 0
	for _, request := range recorded {
		if request.Get(yagoproto.FieldAbstracts) == "" &&
			request.Get(yagoproto.FieldURLs) == "" {
			primaryRequests++
			if request.Get(yagoproto.FieldQuery) != fixture.alpha.String()+fixture.beta.String() {
				t.Fatalf("non-exact primary query = %q", request.Get(yagoproto.FieldQuery))
			}
		}
	}
	if primaryRequests != 1 {
		t.Fatalf("primary requests = %d", primaryRequests)
	}
}

func TestGroupedMorphologyPlanningBoundaries(t *testing.T) {
	if forms := boundedObservedForms(" ", nil); forms != nil {
		t.Fatalf("blank forms = %v", forms)
	}
	candidates := make([]string, maximumObservedFormCandidates+1)
	candidates[0] = ""
	for position := 1; position < len(candidates)-1; position++ {
		candidates[position] = "alpha"
	}
	candidates[len(candidates)-1] = "alphas"
	if forms := boundedObservedForms("alpha", candidates); !slices.Equal(forms, []string{"alpha"}) {
		t.Fatalf("candidate-bound forms = %v", forms)
	}

	requirements, expanded := (searcher{expandWord: func(word string) []string {
		return []string{word, word + "s"}
	}}).groupedMorphologyRequirements([]string{" ", "beta"})
	if expanded || requirements != nil {
		t.Fatalf("partial requirements = %#v, expanded=%v", requirements, expanded)
	}

	alpha := yagomodel.WordHash("alpha")
	beta := yagomodel.WordHash("beta")
	forms := distinctRequirementForms([]queryWordRequirement{
		{forms: []yagomodel.Hash{alpha, beta}},
		{forms: []yagomodel.Hash{beta}},
	})
	if !slices.Equal(forms, []yagomodel.Hash{alpha, beta}) {
		t.Fatalf("distinct forms = %v", forms)
	}
	if urls := intersectRequirementAbstracts(nil, nil); urls != nil {
		t.Fatalf("empty requirements = %v", urls)
	}
	if urls := intersectRequirementAbstracts(
		[]queryWordRequirement{{forms: []yagomodel.Hash{alpha}}, {forms: []yagomodel.Hash{beta}}},
		map[yagomodel.Hash]map[yagomodel.Hash]struct{}{
			alpha: {hashFor("alpha-doc"): {}},
		},
	); urls != nil {
		t.Fatalf("missing requirement abstract = %v", urls)
	}
}

func TestGroupedMorphologyAbstractFanoutIsLinearAndCapped(t *testing.T) {
	t.Run("surface cap", testGroupedMorphologySurfaceCap)
	t.Run("peer cap", testGroupedMorphologyPeerCap)
	t.Run("secondary peer deduplication", testGroupedMorphologyPeerDeduplication)
}

func testGroupedMorphologySurfaceCap(t *testing.T) {
	terms := []string{"one", "two", "three", "four", "five", "six"}
	var requestsMu sync.Mutex
	abstracts := map[string]bool{}
	primary := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form := r.URL.Query()
		if abstract := form.Get(yagoproto.FieldAbstracts); abstract != "" {
			requestsMu.Lock()
			abstracts[abstract] = true
			requestsMu.Unlock()
			term := yagomodel.Hash(abstract)
			writeFixtureResponse(t, w, yagoproto.SearchResponse{
				IndexCount:    map[yagomodel.Hash]int{term: 0},
				IndexAbstract: map[yagomodel.Hash]string{term: "{}"},
			}.Encode().Encode())
			return
		}
		if form.Get(yagoproto.FieldURLs) != "" {
			t.Error("empty intersection started metadata retrieval")
		}
		requestsMu.Lock()
		primary++
		requestsMu.Unlock()
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	_, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers: fakePeerSource{peers: []yagomodel.Seed{
			serverSeed(t, server.URL),
		}},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: DefaultConcurrency,
		ExpandWord: func(word string) []string {
			forms := make([]string, 100)
			for position := range forms {
				forms[position] = word + "variant" + strconv.Itoa(position)
			}
			return forms
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  "one two three four five six",
		Terms:  terms,
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}

	requestsMu.Lock()
	abstractTotal := len(abstracts)
	primaryTotal := primary
	requestsMu.Unlock()
	if abstractTotal != maximumSwarmMorphologySurfaces {
		t.Fatalf(
			"abstract surfaces = %d, want cap %d",
			abstractTotal,
			maximumSwarmMorphologySurfaces,
		)
	}
	if primaryTotal != 1 {
		t.Fatalf("primary requests = %d", primaryTotal)
	}
}

func testGroupedMorphologyPeerCap(t *testing.T) {
	var abstractRequests int
	var requestsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form := r.URL.Query()
		if abstract := form.Get(yagoproto.FieldAbstracts); abstract != "" {
			requestsMu.Lock()
			abstractRequests++
			requestsMu.Unlock()
			term := yagomodel.Hash(abstract)
			writeFixtureResponse(t, w, yagoproto.SearchResponse{
				IndexCount:    map[yagomodel.Hash]int{term: 0},
				IndexAbstract: map[yagomodel.Hash]string{term: "{}"},
			}.Encode().Encode())
			return
		}
		writeFixtureResponse(t, w, yagoproto.SearchResponse{}.Encode().Encode())
	}))
	defer server.Close()

	peers := make([]yagomodel.Seed, maximumMorphologyPeersPerSurface+1)
	for position := range peers {
		peers[position] = serverSeedWithHash(
			t,
			server.URL,
			hashFor("peer"+strconv.Itoa(position)),
		)
	}
	_, err := NewSearcher(Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       fakePeerSource{peers: peers},
		MaxPeers:    len(peers),
		Redundancy:  len(peers),
		Concurrency: DefaultConcurrency,
		ExpandWord: func(word string) []string {
			return []string{word, word + "s"}
		},
	}).Search(t.Context(), searchcore.Request{
		Query:  "alpha beta",
		Terms:  []string{"alpha", "beta"},
		Source: searchcore.SourceGlobal,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}

	requestsMu.Lock()
	got := abstractRequests
	requestsMu.Unlock()
	want := 4 * maximumMorphologyPeersPerSurface
	if got != want {
		t.Fatalf("abstract peer jobs = %d, want %d", got, want)
	}
}

func testGroupedMorphologyPeerDeduplication(t *testing.T) {
	peers := make([]yagomodel.Seed, maximumMorphologyPeersPerSurface)
	for position := range peers {
		peers[position] = serverSeedWithHash(
			t,
			"http://127.0.0.1:"+strconv.Itoa(20000+position),
			hashFor("peer"+strconv.Itoa(position)),
		)
	}
	targets := make([]termPeerTargets, maximumSwarmMorphologySurfaces)
	document := hashFor("document")
	abstracts := termAbstractCatalog{
		peerTerms: make(map[string]map[yagomodel.Hash]map[yagomodel.Hash]struct{}),
	}
	for position := range targets {
		targets[position] = termPeerTargets{
			term:  hashFor("surface-" + strconv.Itoa(position)),
			peers: append([]yagomodel.Seed(nil), peers...),
		}
		for _, peer := range peers {
			abstracts.admit(targets[position].term, peer, []yagomodel.Hash{document})
		}
	}
	jobs := secondarySearchJobs(
		secondarySearchPlan{
			request:       searchcore.Request{},
			targets:       targets,
			urls:          []yagomodel.Hash{document},
			evidenceTerms: []string{"one", "two"},
			abstracts:     abstracts,
		},
		"freeworld",
		DefaultPerPeerTimeout,
	)
	if len(jobs) != len(peers) {
		t.Fatalf("secondary jobs=%d want=%d", len(jobs), len(peers))
	}
	identities := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		identities[peerRankingIdentity(job.peer)] = struct{}{}
	}
	if len(identities) != len(peers) {
		t.Fatalf("secondary peer identities=%d want=%d", len(identities), len(peers))
	}
}
