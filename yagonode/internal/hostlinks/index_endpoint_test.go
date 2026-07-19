package hostlinks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type fixedStatus struct{}

func (fixedStatus) Version(context.Context) string {
	return "1.940"
}

func (fixedStatus) Uptime(context.Context) int {
	return 42
}

type recordingLinks struct {
	consulted bool
	graph     Graph
}

func (r *recordingLinks) IncomingHostLinks(context.Context) Graph {
	r.consulted = true

	return r.graph
}

type decodedIndexResponse struct {
	Version       string                       `json:"version"`
	Uptime        string                       `json:"uptime"`
	Name          string                       `json:"name"`
	RowDefinition string                       `json:"rowdef"`
	Index         map[string][]json.RawMessage `json:"idx"`
}

func TestIndexDefaultsForUnsupportedObject(t *testing.T) {
	links := &recordingLinks{}

	resp, err := endpoint{
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		status:   fixedStatus{},
		links:    links,
	}.Serve(t.Context(), yagoproto.IndexRequest{Object: "other"})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeIndexResponse(t, resp.Body)
	if resp.ContentType != indexContentType {
		t.Fatalf("ContentType = %q", resp.ContentType)
	}
	if links.consulted {
		t.Fatal("host links source was consulted")
	}
	if got.Version != "1.940" || got.Uptime != "42" {
		t.Fatalf("status = %q/%q, want 1.940/42", got.Version, got.Uptime)
	}
	if got.Name != "" || got.RowDefinition != "" {
		t.Fatalf("name/rowdef = %q/%q, want empty", got.Name, got.RowDefinition)
	}
	if len(got.Index) != 0 {
		t.Fatalf("idx = %v, want empty", got.Index)
	}
}

func TestIndexExportsHostReferences(t *testing.T) {
	links := &recordingLinks{
		graph: Graph{
			RowDefinition: "String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}",
			LinkedHosts: []LinkedHost{
				{
					HostHash: "target",
					References: []json.RawMessage{
						json.RawMessage(`{"h":"source","m":"123","c":"2"}`),
					},
				},
			},
		},
	}

	resp, err := endpoint{
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		status:   fixedStatus{},
		links:    links,
	}.Serve(t.Context(), yagoproto.IndexRequest{Object: yagoproto.IndexObjectHost})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeIndexResponse(t, resp.Body)
	if !links.consulted {
		t.Fatal("host links source was not consulted")
	}
	if got.Name != yagoproto.IndexObjectHost {
		t.Fatalf("Name = %q, want host", got.Name)
	}
	if got.RowDefinition != links.graph.RowDefinition {
		t.Fatalf("RowDefinition = %q, want %q", got.RowDefinition, links.graph.RowDefinition)
	}
	refs := got.Index["target"]
	if len(refs) != 1 {
		t.Fatalf("references = %v, want one reference", refs)
	}
	var ref map[string]string
	if err := json.Unmarshal(refs[0], &ref); err != nil {
		t.Fatalf("decode reference: %v", err)
	}
	if ref["h"] != "source" || ref["m"] != "123" || ref["c"] != "2" {
		t.Fatalf("reference = %v", ref)
	}
}

func TestIndexSkipsEmptyHostHash(t *testing.T) {
	graph := Graph{
		LinkedHosts: []LinkedHost{
			{HostHash: "", References: []json.RawMessage{json.RawMessage(`{"h":"ignored"}`)}},
			{HostHash: "target", References: []json.RawMessage{json.RawMessage(`{"h":"source"}`)}},
		},
	}

	index := graphIndex(graph)

	if _, ok := index[""]; ok {
		t.Fatal("empty host hash should be skipped")
	}
	if len(index["target"]) != 1 {
		t.Fatalf("target references = %v", index["target"])
	}
}

func TestIndexReturnsJSONEncodingError(t *testing.T) {
	links := &recordingLinks{
		graph: Graph{
			LinkedHosts: []LinkedHost{
				{HostHash: "target", References: []json.RawMessage{json.RawMessage(`{bad`)}},
			},
		},
	}

	_, err := endpoint{
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		status:   fixedStatus{},
		links:    links,
	}.Serve(t.Context(), yagoproto.IndexRequest{Object: yagoproto.IndexObjectHost})
	if err == nil {
		t.Fatal("expected JSON encoding error")
	}
}

func TestIndexRejectsForeignNetwork(t *testing.T) {
	links := &recordingLinks{}

	resp, err := endpoint{
		identity: nodeidentity.Identity{NetworkName: "freeworld"},
		status:   fixedStatus{},
		links:    links,
	}.Serve(t.Context(), yagoproto.IndexRequest{
		NetworkName: "othernet",
		Object:      yagoproto.IndexObjectHost,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := decodeIndexResponse(t, resp.Body)
	if links.consulted {
		t.Fatal("host links source consulted for foreign network")
	}
	if got.Name != "" || len(got.Index) != 0 {
		t.Fatalf("response = %+v, want default", got)
	}
}

func TestNoIncomingHostLinksReturnsEmptyGraph(t *testing.T) {
	links := NoIncomingHostLinks{}
	graph := links.IncomingHostLinks(t.Context())

	if graph.RowDefinition != HostReferenceRowDefinition {
		t.Fatalf("RowDefinition = %q", graph.RowDefinition)
	}
	if len(graph.LinkedHosts) != 0 {
		t.Fatalf("LinkedHosts = %v, want empty", graph.LinkedHosts)
	}
}

func TestMountServesHostIndexRoute(t *testing.T) {
	mux := http.NewServeMux()
	router := httpguard.NewWireRouter(mux, httpguard.WireGate{
		Guard:   httpguard.NewRequestGuard(1024, time.Second),
		Address: httpguard.NewClientAddressResolver(nil),
	})
	links := &recordingLinks{
		graph: Graph{LinkedHosts: []LinkedHost{{HostHash: "target"}}},
	}
	Mount(router, nodeidentity.Identity{NetworkName: "freeworld"}, fixedStatus{}, links)
	form := yagoproto.IndexRequest{
		NetworkName: "freeworld",
		Object:      yagoproto.IndexObjectHost,
	}.Form()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		yagoproto.PathIndex+"?"+form.Encode(),
		nil,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	got := decodeIndexResponse(t, rec.Body.String())
	if got.Name != yagoproto.IndexObjectHost {
		t.Fatalf("Name = %q, want host", got.Name)
	}
}

func TestHostReferenceRowDefinitionMatchesYaCy(t *testing.T) {
	// The exact rowdef YaCy's idx.json advertises
	// (WebStructureGraph.hostReferenceFactory.getRow().toString()): only the two
	// cardinal columns carry the {b256} encoder, the host-hash column does not, so
	// a YaCy peer parses the columns with matching widths and codecs.
	const want = "String h-6, Cardinal m-4 {b256}, Cardinal c-4 {b256}"
	if HostReferenceRowDefinition != want {
		t.Fatalf("HostReferenceRowDefinition = %q, want %q", HostReferenceRowDefinition, want)
	}
}

func decodeIndexResponse(t *testing.T, body string) decodedIndexResponse {
	t.Helper()

	var got decodedIndexResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode response: %v; body = %q", err, body)
	}

	return got
}
