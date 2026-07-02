package rwi

import (
	"context"
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
