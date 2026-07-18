package yagonode

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
	"github.com/D4rk4/yago/yagonode/internal/searchremote"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
	"github.com/D4rk4/yago/yagoproto"
)

type stockMorphologyPeerSource struct {
	peer yagomodel.Seed
}

func (source stockMorphologyPeerSource) SearchTargetPeers(context.Context) []yagomodel.Seed {
	return []yagomodel.Seed{source.peer}
}

type strictStockMorphologyPeer struct {
	testing              *testing.T
	firstOriginal        yagomodel.Hash
	secondOriginal       yagomodel.Hash
	firstSibling         yagomodel.Hash
	secondSibling        yagomodel.Hash
	document             yagomodel.Hash
	description          string
	requestsMutex        sync.Mutex
	requestedSiblingRows map[yagomodel.Hash]bool
}

func (peer *strictStockMorphologyPeer) serve(w http.ResponseWriter, request *http.Request) {
	form := request.URL.Query()
	if abstract := form.Get(yagoproto.FieldAbstracts); abstract != "" {
		peer.writeAbstract(w, yagomodel.Hash(abstract))
		return
	}
	if urls := form.Get(yagoproto.FieldURLs); urls != "" {
		peer.writeMetadata(w, form.Get(yagoproto.FieldQuery), urls)
		return
	}
	if got := form.Get(
		yagoproto.FieldQuery,
	); got != peer.firstOriginal.String()+peer.secondOriginal.String() {
		peer.testing.Errorf("primary query hashes = %q", got)
	}
	peer.writeResponse(w, yagoproto.SearchResponse{})
}

func (peer *strictStockMorphologyPeer) writeAbstract(
	w http.ResponseWriter,
	term yagomodel.Hash,
) {
	resources := []yagomodel.Hash(nil)
	if term == peer.firstSibling || term == peer.secondSibling {
		resources = []yagomodel.Hash{peer.document}
		peer.requestsMutex.Lock()
		peer.requestedSiblingRows[term] = true
		peer.requestsMutex.Unlock()
	}
	peer.writeResponse(w, yagoproto.SearchResponse{
		IndexCount: map[yagomodel.Hash]int{term: len(resources)},
		IndexAbstract: map[yagomodel.Hash]string{
			term: yagomodel.EncodeSearchIndexAbstract(resources),
		},
	})
}

func (peer *strictStockMorphologyPeer) writeMetadata(
	w http.ResponseWriter,
	query string,
	urls string,
) {
	if query != peer.firstSibling.String() && query != peer.secondSibling.String() {
		peer.testing.Errorf("metadata query hash = %q", query)
	}
	if urls != peer.document.String() {
		peer.testing.Errorf("metadata URL allowlist = %q", urls)
	}
	peer.writeResponse(w, yagoproto.SearchResponse{
		Count: 1,
		Resources: []yagomodel.URIMetadataRow{{Properties: map[string]string{
			yagomodel.URLMetaHash: peer.document.String(),
			yagomodel.URLMetaURL: yagomodel.EncodeBase64WireForm(
				"https://example.test/emergency-powers",
			),
			yagomodel.URLMetaColDescription: yagomodel.EncodeBase64WireForm(peer.description),
		}}},
	})
}

func (peer *strictStockMorphologyPeer) writeResponse(
	w http.ResponseWriter,
	response yagoproto.SearchResponse,
) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.Copy(w, strings.NewReader(response.Encode().Encode())); err != nil {
		peer.testing.Errorf("write stock response: %v", err)
	}
}

func TestSwarmMorphologyExpanderFindsStockPeerSiblingAbsentFromLocalVocabulary(t *testing.T) {
	t.Run("russian", func(t *testing.T) {
		assertStockMorphologySiblingSearch(t, stockMorphologySearchFixture{
			firstOriginal:  "чрезвычайные",
			secondOriginal: "полномочия",
			firstSibling:   "чрезвычайных",
			secondSibling:  "полномочий",
			description:    "Чрезвычайных полномочий",
			vocabulary: map[string]int{
				"чрезвычайные": 20, "чрезвычайная": 19,
				"чрезвычайному": 18, "чрезвычайной": 17,
				"полномочия": 20, "полномочиями": 19,
				"полномочию": 18, "полномочиям": 17,
			},
		})
	})
	t.Run("german-and-english", func(t *testing.T) {
		assertStockMorphologySiblingSearch(t, stockMorphologySearchFixture{
			firstOriginal:  "häuser",
			secondOriginal: "house",
			firstSibling:   "häusern",
			secondSibling:  "houses",
			description:    "Häusern houses",
			vocabulary: map[string]int{
				"häuser": 20,
				"house":  20,
			},
		})
	})
	t.Run("german-unchanged-stem", func(t *testing.T) {
		assertStockMorphologySiblingSearch(t, stockMorphologySearchFixture{
			firstOriginal:  "kind",
			secondOriginal: "house",
			firstSibling:   "kinder",
			secondSibling:  "houses",
			description:    "Kinder houses",
			vocabulary: map[string]int{
				"kind":  20,
				"house": 20,
			},
		})
	})
}

type stockMorphologySearchFixture struct {
	firstOriginal  string
	secondOriginal string
	firstSibling   string
	secondSibling  string
	description    string
	vocabulary     map[string]int
}

func assertStockMorphologySiblingSearch(
	t *testing.T,
	input stockMorphologySearchFixture,
) {
	t.Helper()
	holder := wordforms.NewHolder()
	holder.Store(wordforms.New(input.vocabulary, searchindex.StemWord))
	for source, absent := range map[string]string{
		input.firstOriginal:  input.firstSibling,
		input.secondOriginal: input.secondSibling,
	} {
		if slices.Contains(holder.Current().Variants(source), absent) {
			t.Fatalf("local vocabulary unexpectedly contains %q", absent)
		}
	}

	fixture := &strictStockMorphologyPeer{
		testing:              t,
		firstOriginal:        yagomodel.WordHash(input.firstOriginal),
		secondOriginal:       yagomodel.WordHash(input.secondOriginal),
		firstSibling:         yagomodel.WordHash(input.firstSibling),
		secondSibling:        yagomodel.WordHash(input.secondSibling),
		document:             stockMorphologyHash("peer-document"),
		description:          input.description,
		requestedSiblingRows: make(map[yagomodel.Hash]bool),
	}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()

	peer := stockMorphologySeed(t, server.URL)
	response, err := searchremote.NewSearcher(searchremote.Config{
		Client:      server.Client(),
		NetworkName: "freeworld",
		Peers:       stockMorphologyPeerSource{peer: peer},
		MaxPeers:    1,
		Redundancy:  1,
		Concurrency: 8,
		ExpandWord: swarmMorphologyExpander(publicSearchAssembly{
			swarmMorphology: true,
			wordForms:       holder.Current,
		}),
	}).Search(t.Context(), searchcore.Request{
		Query:         input.firstOriginal + " " + input.secondOriginal,
		Terms:         []string{input.firstOriginal, input.secondOriginal},
		Source:        searchcore.SourceGlobal,
		Limit:         10,
		ContentDomain: searchcore.ContentDomainText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].URLHash != fixture.document.String() {
		t.Fatalf("stock peer response = %#v", response)
	}
	fixture.requestsMutex.Lock()
	firstRequested := fixture.requestedSiblingRows[fixture.firstSibling]
	secondRequested := fixture.requestedSiblingRows[fixture.secondSibling]
	fixture.requestsMutex.Unlock()
	if !firstRequested || !secondRequested {
		t.Fatalf("requested sibling hashes = %#v", fixture.requestedSiblingRows)
	}
}

func stockMorphologySeed(t *testing.T, raw string) yagomodel.Seed {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host, portValue, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	parsedHost, err := yagomodel.ParseHost(host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		t.Fatal(err)
	}

	return yagomodel.Seed{
		Hash:     stockMorphologyHash("stock-peer"),
		IP:       yagomodel.Some(parsedHost),
		Port:     yagomodel.Some(yagomodel.Port(port)),
		Flags:    yagomodel.Some(yagomodel.ZeroFlags().Set(yagomodel.FlagAcceptRemoteIndex, true)),
		RWICount: yagomodel.Some(1),
	}
}

func stockMorphologyHash(value string) yagomodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(value) >= yagomodel.HashLength {
		return yagomodel.Hash(value[:yagomodel.HashLength])
	}

	return yagomodel.Hash(value + filler[len(value):])
}
