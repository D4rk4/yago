package yagonode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

type webSeedPublicationQueue struct {
	calls      int
	identities []string
}

func (q *webSeedPublicationQueue) PublishOnce(
	_ context.Context,
	identity string,
	_ yagocrawlcontract.CrawlOrder,
) (bool, error) {
	q.calls++
	q.identities = append(q.identities, identity)
	switch {
	case strings.Contains(identity, "coalesced"):
		return true, nil
	case strings.Contains(identity, "failed"):
		return false, fmt.Errorf("PRIVATE_ERROR_CANARY https://PRIVATE_URL_CANARY_ERROR/")
	default:
		return false, nil
	}
}

func TestWebSeedOutcomeDistinguishesPublishedCoalescedAndFailed(t *testing.T) {
	previous := slog.Default()
	var output concurrentLogCapture
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	queue := &webSeedPublicationQueue{}
	seeder := newWebCrawlSeeder(
		queue,
		fakeSeedDocuments{
			stored: map[string]bool{"https://stored.example/PRIVATE_URL_CANARY_STORED": true},
		},
		yagomodel.Hash("node"),
		webCrawlSeedProfile{fallback: webFallbackConfig{SeedMaxPages: 1}},
	)
	requestContext, _ := tracectx.StartServerSpan(
		t.Context(),
		"00-11111111111111112222222222222222-3333333333333333-01",
	)
	serverSpanID, ok := tracectx.ServerSpanIDFromContext(requestContext)
	if !ok {
		t.Fatal("server span ID missing")
	}
	ctx := tracectx.CopyServerSpan(context.Background(), requestContext)
	if _, ok := tracectx.FromContext(ctx); ok {
		t.Fatal("background seed context copied the request trace")
	}
	seeder.Seed(ctx, []string{
		"https://published.example/PRIVATE_URL_CANARY_PUBLISHED",
		"https://coalesced.example/PRIVATE_URL_CANARY_COALESCED",
		"https://failed.example/PRIVATE_URL_CANARY_FAILED",
		"https://stored.example/PRIVATE_URL_CANARY_STORED",
		"not-a-url",
	})
	if queue.calls != 5 {
		t.Fatalf(
			"publish calls = %d, want one each for success/coalescing and three failure attempts",
			queue.calls,
		)
	}
	assertWebSeedPublicationBranches(t, queue)
	assertWebSeedPublicationLog(t, output.String(), serverSpanID)
}

func assertWebSeedPublicationBranches(t *testing.T, queue *webSeedPublicationQueue) {
	t.Helper()
	if countSeedIdentity(queue.identities, "published") != 1 ||
		countSeedIdentity(queue.identities, "coalesced") != 1 ||
		countSeedIdentity(queue.identities, "failed") != 3 ||
		countSeedIdentity(queue.identities, "stored") != 0 ||
		countSeedIdentity(queue.identities, "not-a-url") != 0 {
		t.Fatalf("seed admission branches changed: %d publish attempts", len(queue.identities))
	}
}

func assertWebSeedPublicationLog(t *testing.T, logOutput, serverSpanID string) {
	t.Helper()
	for _, field := range []string{
		`"urls":5`,
		`"published":1`,
		`"coalesced":1`,
		`"failed":1`,
		`"alreadyStored":1`,
		`"unusableUrl":1`,
	} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("web seed outcome log %q does not contain %s", logOutput, field)
		}
	}
	for _, field := range []string{
		`"outcome":"published"`,
		`"outcome":"coalesced"`,
		`"outcome":"failed"`,
		`"cause":"backend"`,
		`"serverSpanId":"` + serverSpanID + `"`,
	} {
		if !strings.Contains(logOutput, field) {
			t.Fatalf("web seed publication logs %q do not contain %s", logOutput, field)
		}
	}
	outcomeEvents := 0
	for _, line := range strings.Split(strings.TrimSpace(logOutput), "\n") {
		entry := map[string]any{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode web seed log: %v", err)
		}
		if entry["msg"] != msgWebSeedOutcome {
			continue
		}
		outcomeEvents++
		if entry["serverSpanId"] != serverSpanID {
			t.Errorf(
				"web seed outcome serverSpanId = %v, want %s",
				entry["serverSpanId"],
				serverSpanID,
			)
		}
	}
	if outcomeEvents != 1 {
		t.Fatalf("web seed outcome events = %d, want 1", outcomeEvents)
	}
	for _, canary := range []string{
		"PRIVATE_URL_CANARY_PUBLISHED",
		"PRIVATE_URL_CANARY_COALESCED",
		"PRIVATE_URL_CANARY_FAILED",
		"PRIVATE_URL_CANARY_STORED",
		"PRIVATE_ERROR_CANARY",
		"PRIVATE_URL_CANARY_ERROR",
		"11111111111111112222222222222222",
		"3333333333333333",
	} {
		if strings.Contains(logOutput, canary) {
			t.Fatalf("web seed publication log exposed %q: %s", canary, logOutput)
		}
	}
}

func countSeedIdentity(identities []string, fragment string) int {
	count := 0
	for _, identity := range identities {
		if strings.Contains(identity, fragment) {
			count++
		}
	}

	return count
}
