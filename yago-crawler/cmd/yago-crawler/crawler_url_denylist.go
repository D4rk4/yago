package main

import "github.com/D4rk4/yago/yago-crawler/internal/crawldenylist"

func applyCrawlURLDenylist(
	chains fetchChains,
	denylist *crawldenylist.Denylist,
) fetchChains {
	chains.verifying = crawldenylist.NewAdmissionFetcher(chains.verifying, denylist)
	chains.insecure = crawldenylist.NewAdmissionFetcher(chains.insecure, denylist)
	chains.verifyingDirect = crawldenylist.NewAdmissionFetcher(
		chains.verifyingDirect,
		denylist,
	)
	chains.insecureDirect = crawldenylist.NewAdmissionFetcher(
		chains.insecureDirect,
		denylist,
	)

	return chains
}
