package main

import (
	"github.com/D4rk4/yago/yagocrawler/internal/crawlorder"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
)

func assembleCrawlerControlHandler(
	restart func(),
	concurrency *workerConcurrency,
	crawlFrontier *frontier.Frontier,
) crawlorder.ControlHandler {
	return crawlorder.NewRestartControlHandler(
		restart,
		crawlorder.NewWorkerConcurrencyControlHandler(
			concurrency.Set,
			crawlorder.NewAutomaticDiscoveryPriorityControlHandler(
				crawlFrontier.SetAutomaticDiscoveryPriority,
				crawlorder.NewFrontierControlHandler(crawlFrontier),
			),
		),
	)
}
