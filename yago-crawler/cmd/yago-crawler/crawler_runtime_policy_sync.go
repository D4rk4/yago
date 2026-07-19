package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

var synchronizeCrawlerRuntimePolicy = readCrawlerRuntimePolicy

func readCrawlerRuntimePolicy(
	ctx context.Context,
	config ServiceConfig,
) (ServiceConfig, error) {
	client, closer, err := newCrawlerExchange(config.NodeRPCAddr)
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("open crawler policy exchange: %w", err)
	}
	defer func() { _ = closer.Close() }()
	readContext, cancelRead := context.WithTimeout(ctx, config.Crawl.ConnectTimeout)
	defer cancelRead()
	message, err := client.ReadRuntimePolicy(
		readContext,
		&crawlrpc.CrawlerRuntimePolicyRequest{WorkerId: config.WorkerID},
	)
	if status.Code(err) == codes.Unimplemented {
		return config, nil
	}
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("read crawler runtime policy: %w", err)
	}
	policy, err := yagocrawlcontract.CrawlerRuntimePolicyFromProtoWithFallback(
		message,
		config.runtimePolicy(),
	)
	if err != nil {
		return ServiceConfig{}, fmt.Errorf("decode crawler runtime policy: %w", err)
	}

	return config.withRuntimePolicy(policy), nil
}

func restartOnCrawlerRuntimePolicyChange(
	effective yagocrawlcontract.CrawlerRuntimePolicy,
	restart func(),
) func(yagocrawlcontract.CrawlerRuntimePolicy) {
	return newCrawlerRuntimePolicyChange(effective, nil, restart).Apply
}
