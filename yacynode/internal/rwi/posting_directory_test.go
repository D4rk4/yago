package rwi

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
)

func TestScanWordVisitsMatchingPostings(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u3"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	word := yacymodel.WordHash("w1")
	var visited []yacymodel.RWIPosting
	err := h.rwi.Index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
		visited = append(visited, entry)

		return true, nil
	})
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if len(visited) != 2 {
		t.Fatalf("visited %d postings, want 2", len(visited))
	}
	for _, entry := range visited {
		if entry.WordHash != word {
			t.Fatalf("entry word hash = %q, want %q", entry.WordHash, word)
		}
	}
}

func TestRWIURLCountCountsOneWord(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u3"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	count, err := h.rwi.Index.RWIURLCount(ctx, yacymodel.WordHash("w1"))
	if err != nil {
		t.Fatalf("RWIURLCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("RWIURLCount = %d, want 2", count)
	}
}

func TestRWIURLCountReturnsScanError(t *testing.T) {
	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	engine.scanErrors[postingsBucket] = errors.New("scan failed")

	if _, err := index.RWIURLCount(t.Context(), yacymodel.WordHash("w1")); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestScanWordStopsWhenVisitorStops(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	visited := 0
	err := h.rwi.Index.ScanWord(
		ctx,
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			visited++

			return false, nil
		},
	)
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited %d postings, want 1 before stop", visited)
	}
}
