package main

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

func assembleCrawlerControlHandler(
	restart func(),
	concurrency *workerConcurrency,
	resizeActiveRuns func(int),
	crawlFrontier *frontier.Frontier,
) crawlorder.ControlHandler {
	return crawlorder.NewRestartControlHandler(
		restart,
		crawlorder.NewWorkerConcurrencyControlHandler(
			concurrency.Set,
			crawlorder.NewActiveRunControlHandler(
				resizeActiveRuns,
				crawlorder.NewAutomaticDiscoveryPriorityControlHandler(
					crawlFrontier.SetAutomaticDiscoveryPriority,
					crawlorder.NewFrontierControlHandler(crawlFrontier),
				),
			),
		),
	)
}
