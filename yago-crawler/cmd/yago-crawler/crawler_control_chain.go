package main

import (
	"github.com/D4rk4/yago/yago-crawler/internal/crawlorder"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
)

type crawlerControlActions struct {
	restart             func()
	concurrency         *workerConcurrency
	setProcessRate      func(uint32)
	setMaximumRedirects func(int)
	resizeActiveRuns    func(int)
	frontier            *frontier.Frontier
}

func assembleCrawlerControlHandler(actions crawlerControlActions) crawlorder.ControlHandler {
	return crawlorder.NewRestartControlHandler(
		actions.restart,
		crawlorder.NewWorkerConcurrencyControlHandler(
			actions.concurrency.Set,
			crawlorder.NewProcessRateControl(
				actions.setProcessRate,
				crawlorder.NewMaximumRedirectsControl(
					actions.setMaximumRedirects,
					crawlorder.NewActiveRunControlHandler(
						actions.resizeActiveRuns,
						crawlorder.NewAutomaticDiscoveryPriorityControlHandler(
							actions.frontier.SetAutomaticDiscoveryPriority,
							crawlorder.NewFrontierControlHandler(actions.frontier),
						),
					),
				),
			),
		),
	)
}
