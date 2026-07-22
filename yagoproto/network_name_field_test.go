package yagoproto_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/D4rk4/yago/yagoproto"
)

type networkNamePresenceSnapshot struct {
	name    string
	present bool
	form    url.Values
}

type networkNamePresenceParser func(
	context.Context,
	url.Values,
) (networkNamePresenceSnapshot, error)

type authenticatedRequestNetworkCase struct {
	name  string
	form  url.Values
	parse networkNamePresenceParser
}

func TestAuthenticatedRequestNetworkNamePresenceRoundTrips(t *testing.T) {
	t.Parallel()

	for _, test := range authenticatedRequestNetworkCases(t) {
		t.Run(test.name, func(t *testing.T) {
			assertNetworkNamePresenceRoundTrip(t, test.form, test.parse)
		})
	}
}

func authenticatedRequestNetworkCases(t *testing.T) []authenticatedRequestNetworkCase {
	t.Helper()

	caller := sampleHash(t, "caller")
	target := sampleHash(t, "target")

	return []authenticatedRequestNetworkCase{
		{
			name: "hello",
			form: (yagoproto.HelloRequest{
				Seed: sampleSeed(t, "caller", "peer-caller"),
			}).Form(),
			parse: helloNetworkNamePresence,
		},
		{
			name:  "crawl URLs",
			form:  (yagoproto.CrawlURLRequest{}).Form(),
			parse: crawlURLNetworkNamePresence,
		},
		{
			name:  "search",
			form:  (yagoproto.SearchRequest{}).Form(),
			parse: searchNetworkNamePresence,
		},
		{
			name:  "host index",
			form:  (yagoproto.IndexRequest{}).Form(),
			parse: indexNetworkNamePresence,
		},
		{
			name:  "query",
			form:  (yagoproto.QueryRequest{}).Form(),
			parse: queryNetworkNamePresence,
		},
		{
			name:  "profile",
			form:  (yagoproto.ProfileRequest{}).Form(),
			parse: profileNetworkNamePresence,
		},
		{
			name: "RWI transfer",
			form: (yagoproto.TransferRWIRequest{
				Iam: caller, YouAre: target,
			}).Form(),
			parse: transferRWINetworkNamePresence,
		},
		{
			name:  "shared list",
			form:  (yagoproto.ListRequest{}).Form(),
			parse: listNetworkNamePresence,
		},
		{
			name: "URL transfer",
			form: (yagoproto.TransferURLRequest{
				Iam: caller, YouAre: target,
			}).Form(),
			parse: transferURLNetworkNamePresence,
		},
		{
			name:  "crawl receipt",
			form:  (yagoproto.CrawlReceiptRequest{}).Form(),
			parse: crawlReceiptNetworkNamePresence,
		},
		{
			name:  "peer message",
			form:  (yagoproto.MessageRequest{YouAre: target}).Form(),
			parse: messageNetworkNamePresence,
		},
	}
}

func helloNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseHelloRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func crawlURLNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseCrawlURLRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func searchNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseSearchRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func indexNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseIndexRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func queryNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseQueryRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func profileNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseProfileRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func transferRWINetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseTransferRWIRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func listNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseListRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func transferURLNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseTransferURLRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func crawlReceiptNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseCrawlReceiptRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func messageNetworkNamePresence(
	ctx context.Context,
	form url.Values,
) (networkNamePresenceSnapshot, error) {
	request, err := yagoproto.ParseMessageRequest(ctx, form)

	return authenticatedNetworkNameSnapshot(networkNamePresenceSnapshot{
		name: request.NetworkName, present: request.NetworkNamePresent, form: request.Form(),
	}, err)
}

func authenticatedNetworkNameSnapshot(
	snapshot networkNamePresenceSnapshot,
	err error,
) (networkNamePresenceSnapshot, error) {
	if err != nil {
		return networkNamePresenceSnapshot{}, fmt.Errorf("parse authenticated request: %w", err)
	}

	return snapshot, nil
}

func assertNetworkNamePresenceRoundTrip(
	t *testing.T,
	base url.Values,
	parse networkNamePresenceParser,
) {
	t.Helper()

	for _, present := range []bool{false, true} {
		form := make(url.Values, len(base))
		for key, values := range base {
			form[key] = append([]string(nil), values...)
		}
		form.Del(yagoproto.FieldNetworkName)
		if present {
			form.Set(yagoproto.FieldNetworkName, "")
		}
		snapshot, err := parse(t.Context(), form)
		if err != nil {
			t.Fatalf("parse present=%v: %v", present, err)
		}
		if snapshot.name != "" || snapshot.present != present {
			t.Fatalf(
				"parsed network = %q, %v; want empty, %v",
				snapshot.name,
				snapshot.present,
				present,
			)
		}
		if snapshot.form.Has(yagoproto.FieldNetworkName) != present ||
			snapshot.form.Get(yagoproto.FieldNetworkName) != "" {
			t.Fatalf("round-trip form = %v; want presence %v", snapshot.form, present)
		}
	}
}
