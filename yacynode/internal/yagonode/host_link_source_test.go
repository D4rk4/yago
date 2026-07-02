package yagonode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/hostlinks"
)

type storedURLMetadataRowsScript struct {
	rows []yacymodel.URIMetadataRow
	err  error
}

func (s storedURLMetadataRowsScript) StoredURLMetadataRows(
	ctx context.Context,
	visit func(yacymodel.URIMetadataRow) (bool, error),
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if s.err != nil {
		return s.err
	}
	for _, row := range s.rows {
		again, err := visit(row)
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}

	return nil
}

func TestStoredURLHostLinksReturnsRowDefinitionWithoutRows(t *testing.T) {
	graph := storedURLHostLinks{}.IncomingHostLinks(t.Context())

	if graph.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf(
			"RowDefinition = %q, want %q",
			graph.RowDefinition,
			hostlinks.HostReferenceRowDefinition,
		)
	}
	if len(graph.LinkedHosts) != 0 {
		t.Fatalf("LinkedHosts = %v, want empty", graph.LinkedHosts)
	}
}

func TestStoredURLHostLinksBuildsIncomingGraph(t *testing.T) {
	targetA := urlHashForTest(t, "https://target.example/a")
	targetB := urlHashForTest(t, "https://target.example/b")
	sourceA := urlHashForTest(t, "https://source.example/home")
	sourceB := urlHashForTest(t, "https://other.example/home")
	rows := []yacymodel.URIMetadataRow{
		hostLinkRow(targetA, sourceA, map[string]string{yacymodel.ColLoadDate: "20260102"}),
		hostLinkRow(targetB, sourceA, map[string]string{yacymodel.ColModDate: "20260103"}),
		hostLinkRow(targetA, sourceB, map[string]string{yacymodel.ColFreshDate: "20260104"}),
		hostLinkRow(targetA, targetB, nil),
		{Properties: map[string]string{}},
		hostLinkRow(targetA, "", nil),
	}

	graph := storedURLHostLinks{
		rows: storedURLMetadataRowsScript{rows: rows},
	}.IncomingHostLinks(t.Context())

	targetHost := hostHashForTest(t, targetA)
	sourceAHost := hostHashForTest(t, sourceA)
	sourceBHost := hostHashForTest(t, sourceB)
	if len(graph.LinkedHosts) != 1 {
		t.Fatalf("LinkedHosts = %v, want one target host", graph.LinkedHosts)
	}
	if graph.LinkedHosts[0].HostHash != targetHost {
		t.Fatalf("HostHash = %q, want %q", graph.LinkedHosts[0].HostHash, targetHost)
	}
	got := decodedHostReferences(t, graph.LinkedHosts[0].References)
	if got[sourceAHost].Count != "2" {
		t.Fatalf("source A count = %q, want 2", got[sourceAHost].Count)
	}
	if got[sourceAHost].ModifiedDay != fmt.Sprint(hostLinkModifiedDay(rows[1])) {
		t.Fatalf("source A modified day = %q", got[sourceAHost].ModifiedDay)
	}
	if got[sourceBHost].Count != "1" {
		t.Fatalf("source B count = %q, want 1", got[sourceBHost].Count)
	}
	if got[sourceBHost].ModifiedDay != fmt.Sprint(hostLinkModifiedDay(rows[2])) {
		t.Fatalf("source B modified day = %q", got[sourceBHost].ModifiedDay)
	}
}

func TestStoredURLHostLinksCapsGraphResponse(t *testing.T) {
	target := urlHashForTest(t, "https://target.example/a")
	rows := make([]yacymodel.URIMetadataRow, 0, hostLinkMaxReferencesPerHost+1)
	for i := range hostLinkMaxReferencesPerHost + 1 {
		source := urlHashForTest(t, fmt.Sprintf("https://source-%03d.example/", i))
		rows = append(rows, hostLinkRow(target, source, nil))
	}
	for i := range hostLinkMaxLinkedHosts + 1 {
		rowTarget := urlHashForTest(t, fmt.Sprintf("https://target-%05d.example/", i))
		source := urlHashForTest(t, fmt.Sprintf("https://overflow-source-%05d.example/", i))
		rows = append(rows, hostLinkRow(rowTarget, source, nil))
	}

	graph := storedURLHostLinks{
		rows: storedURLMetadataRowsScript{rows: rows},
	}.IncomingHostLinks(t.Context())

	if len(graph.LinkedHosts) != hostLinkMaxLinkedHosts {
		t.Fatalf("LinkedHosts = %d, want %d", len(graph.LinkedHosts), hostLinkMaxLinkedHosts)
	}
	for _, linked := range graph.LinkedHosts {
		if linked.HostHash == hostHashForTest(t, target) &&
			len(linked.References) != hostLinkMaxReferencesPerHost {
			t.Fatalf(
				"references = %d, want %d",
				len(linked.References),
				hostLinkMaxReferencesPerHost,
			)
		}
	}
}

func TestStoredURLHostLinksReturnsEmptyGraphOnScanError(t *testing.T) {
	graph := storedURLHostLinks{
		rows: storedURLMetadataRowsScript{err: errors.New("scan failed")},
	}.IncomingHostLinks(t.Context())

	if graph.RowDefinition != hostlinks.HostReferenceRowDefinition {
		t.Fatalf("RowDefinition = %q", graph.RowDefinition)
	}
	if len(graph.LinkedHosts) != 0 {
		t.Fatalf("LinkedHosts = %v, want empty after scan error", graph.LinkedHosts)
	}
}

func TestHostLinkModifiedDayRejectsBadFreshness(t *testing.T) {
	row := yacymodel.URIMetadataRow{
		Properties: map[string]string{yacymodel.ColLoadDate: "not-a-day"},
	}

	if got := hostLinkModifiedDay(row); got != 0 {
		t.Fatalf("ModifiedDay = %d, want 0", got)
	}
}

type decodedHostReference struct {
	HostHash    string `json:"h"`
	ModifiedDay string `json:"m"`
	Count       string `json:"c"`
}

func decodedHostReferences(
	t *testing.T,
	messages []json.RawMessage,
) map[string]decodedHostReference {
	t.Helper()

	decoded := map[string]decodedHostReference{}
	for _, message := range messages {
		var reference decodedHostReference
		if err := json.Unmarshal(message, &reference); err != nil {
			t.Fatalf("decode reference: %v", err)
		}
		decoded[reference.HostHash] = reference
	}

	return decoded
}

func hostLinkRow(
	hash yacymodel.URLHash,
	referrer yacymodel.URLHash,
	extra map[string]string,
) yacymodel.URIMetadataRow {
	props := map[string]string{yacymodel.URLMetaHash: hash.String()}
	if referrer != "" {
		props[yacymodel.URLMetaReferrer] = referrer.String()
	}
	for key, value := range extra {
		props[key] = value
	}

	return yacymodel.URIMetadataRow{Properties: props}
}

func urlHashForTest(t *testing.T, rawURL string) yacymodel.URLHash {
	t.Helper()

	hash, err := yacymodel.HashURL(rawURL)
	if err != nil {
		t.Fatalf("HashURL(%q): %v", rawURL, err)
	}

	return hash
}

func hostHashForTest(t *testing.T, hash yacymodel.URLHash) string {
	t.Helper()

	host, err := hash.HostHash()
	if err != nil {
		t.Fatalf("HostHash(%q): %v", hash, err)
	}

	return host
}
