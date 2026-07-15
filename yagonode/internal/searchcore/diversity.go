package searchcore

const siteCrowdingLimit = 2

type resultSiteAppearance struct {
	frequency   int
	registrable bool
}

func DiversifyResults(results []Result, req Request) []Result {
	consolidated := ConsolidateClusters(results)
	if req.SiteHost != "" || req.SortByDate {
		return consolidated
	}

	return deferCrowdedSites(rerankMarginalRelevance(consolidated))
}

func deferCrowdedSites(results []Result) []Result {
	siteAppearances := make(map[string]resultSiteAppearance, len(results))
	head := make([]Result, 0, len(results))
	var deferredOrdinals []int
	for ordinal, result := range results {
		host := normalizedResultHost(result.Host)
		site := host
		appearance, observed := siteAppearances[site]
		registrable := appearance.registrable
		if !observed {
			parentSite := observedRegistrableParentSite(host, siteAppearances)
			if parentSite != "" {
				site = parentSite
				appearance = siteAppearances[site]
				registrable = true
			} else {
				site, registrable = registrableResultSiteIdentity(host)
				appearance = siteAppearances[site]
			}
		}
		if site != "" && appearance.frequency >= siteCrowdingLimit {
			if deferredOrdinals == nil {
				deferredOrdinals = make([]int, 0, len(results)-ordinal)
			}
			deferredOrdinals = append(deferredOrdinals, ordinal)

			continue
		}
		appearance.frequency++
		appearance.registrable = appearance.registrable || registrable
		siteAppearances[site] = appearance
		head = append(head, result)
	}
	for _, ordinal := range deferredOrdinals {
		head = append(head, results[ordinal])
	}

	return head
}
