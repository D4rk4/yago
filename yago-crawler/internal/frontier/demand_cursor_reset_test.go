package frontier

import (
	"context"
	"testing"
)

func TestResetSearchDemandCursorAllowsNodeFeedRecovery(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	if err := frontier.SaveSearchDemandCursor(ctx, "55"); err != nil {
		t.Fatalf("SaveSearchDemandCursor: %v", err)
	}
	if err := frontier.ResetSearchDemandCursor(ctx); err != nil {
		t.Fatalf("ResetSearchDemandCursor: %v", err)
	}
	cursor, err := frontier.SearchDemandCursor(ctx)
	if err != nil || cursor != "0" {
		t.Fatalf("reset cursor = %q, %v", cursor, err)
	}
	if err := frontier.SaveSearchDemandCursor(ctx, "6"); err != nil {
		t.Fatalf("save cursor after reset: %v", err)
	}
}
