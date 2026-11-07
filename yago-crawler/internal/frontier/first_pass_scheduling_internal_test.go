package frontier

import (
	"testing"

	"github.com/google/uuid"
)

func TestFirstPassRunSelectionRejectsMissingRun(t *testing.T) {
	frontier := NewFrontier(1, nil)
	frontier.mu.Lock()
	selected := firstPassRunSelectable(frontier, uuid.New(), nil)
	frontier.mu.Unlock()
	if selected {
		t.Fatal("missing run was selected for first-pass work")
	}
}
