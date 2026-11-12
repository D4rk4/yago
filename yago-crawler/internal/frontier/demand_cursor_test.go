package frontier

import (
	"context"
	"testing"
)

func TestSearchDemandCursorPersistsAndRejectsBackwardMovement(t *testing.T) {
	frontier := openFrontier(t, 10)
	ctx := context.Background()
	cursor, err := frontier.SearchDemandCursor(ctx)
	if err != nil || cursor != "0" {
		t.Fatalf("initial cursor = %q, %v", cursor, err)
	}
	if err := frontier.SaveSearchDemandCursor(ctx, "42"); err != nil {
		t.Fatalf("SaveSearchDemandCursor: %v", err)
	}
	cursor, err = frontier.SearchDemandCursor(ctx)
	if err != nil || cursor != "42" {
		t.Fatalf("saved cursor = %q, %v", cursor, err)
	}
	if err := frontier.SaveSearchDemandCursor(ctx, "41"); err == nil {
		t.Fatal("backward cursor movement was accepted")
	}
	if err := frontier.SaveSearchDemandCursor(ctx, "not-a-sequence"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}
