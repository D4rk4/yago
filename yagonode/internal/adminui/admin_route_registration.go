package adminui

import "io/fs"

func (c *Console) registerAssetAndSearchRoutes(assets fs.FS) {
	c.mux.Handle("GET /admin/assets/", assetHandler(assets, embeddedAdminAssetCatalog))
	if c.searchSuggest != nil {
		c.mux.Handle("GET /admin/search/suggest", c.searchSuggest)
	}
	c.mux.HandleFunc("GET /admin/{$}", handleRoot)
	c.mux.HandleFunc("GET "+overviewPath, c.handleOverview)
	c.mux.HandleFunc("GET "+overviewMetricsPath, c.handleOverviewMetrics)
	c.mux.HandleFunc("GET "+systemMonitorPath, c.handleSystemMonitor)
	c.mux.HandleFunc("GET "+searchPath, c.handleSearch)
	c.mux.HandleFunc("GET "+activityPath, c.handleActivity)
}

func (c *Console) registerCrawlRoutes() {
	c.mux.HandleFunc("GET "+crawlPath, c.handleCrawl)
	c.mux.HandleFunc("POST "+crawlPath, c.handleCrawlStart)
	c.mux.HandleFunc("POST "+crawlPath+"/formats", c.handleCrawlFormats)
	c.mux.HandleFunc("POST "+crawlSchedulePath, c.handleCrawlSchedule)
	c.mux.HandleFunc("POST "+crawlProfilePath, c.handleSavedCrawlProfile)
	c.mux.HandleFunc("GET "+crawlRunPath, c.handleCrawlRunDetail)
	c.mux.HandleFunc("GET "+crawlMonitorPath, c.handleCrawlMonitor)
	c.mux.HandleFunc("POST "+crawlControlPath, c.handleCrawlControl)
	c.mux.HandleFunc("GET "+autocrawlerPath, handleAutocrawlerRedirect)
	c.mux.HandleFunc("POST "+autocrawlerPath, handleAutocrawlerRedirect)
	c.mux.HandleFunc("POST "+autocrawlerPath+"/formats", handleAutocrawlerRedirect)
}

func (c *Console) registerIndexAndNetworkRoutes() {
	c.mux.HandleFunc("GET "+indexPath, c.handleIndex)
	c.mux.HandleFunc("GET "+indexDocumentPath, c.handleIndexDocument)
	c.mux.HandleFunc("POST "+indexDeletePath, c.handleIndexDelete)
	c.mux.HandleFunc("POST "+indexRebuildPath, c.handleIndexRebuild)
	c.mux.HandleFunc("POST "+extractionRecrawlPath, c.handleExtractionRecrawl)
	c.mux.HandleFunc("POST "+blacklistPath, c.handleBlacklist)
	c.mux.HandleFunc("GET "+blacklistTestPath, c.handleBlacklistTest)
	c.mux.HandleFunc("GET "+blacklistExportPath, c.handleBlacklistExport)
	c.mux.HandleFunc("POST "+blacklistImportPath, c.handleBlacklistImport)
	c.mux.HandleFunc("GET "+indexExportPath, c.handleIndexExport)
	c.mux.HandleFunc("GET "+networkPath, c.handleNetwork)
	c.mux.HandleFunc("POST "+networkSelfTestPath, c.handleNetworkSelfTest)
	c.mux.HandleFunc("GET "+networkPeerPath, c.handleNetworkPeer)
	c.mux.HandleFunc("POST "+peerBlockPath, c.handlePeerBlock)
	c.mux.HandleFunc("POST "+seedlistRefreshPath, c.handleSeedlistRefresh)
}

func (c *Console) registerOperatorRoutes() {
	c.mux.HandleFunc("GET "+configPath, c.handleConfig)
	c.mux.HandleFunc("POST "+configPath, c.handleConfigUpdate)
	c.mux.HandleFunc("POST "+configPath+"/formats", c.handleConfigFormats)
	c.mux.HandleFunc("GET "+logsPath, c.handleLogs)
	c.mux.HandleFunc("GET "+logsEventsPath, c.handleLogsEvents)
	c.mux.HandleFunc("GET "+securityPath, c.handleSecurity)
	c.mux.HandleFunc("POST "+securityPath, c.handleSecurityUpdate)
	c.mux.HandleFunc("GET "+restartPath, c.handleRestartPage)
	c.mux.HandleFunc("POST "+restartPath, c.handleRestartAction)
	c.mux.HandleFunc("GET "+portalPath, c.handlePortal)
	c.mux.HandleFunc("POST "+portalPath, c.handlePortalUpdate)
	c.mux.HandleFunc("POST "+portalPath+"/design", c.handlePortalDesign)
	c.mux.HandleFunc("GET "+performancePath, c.handlePerformance)
	c.mux.HandleFunc("GET "+yagorankPath, c.handleYagoRank)
	c.mux.HandleFunc("POST "+yagorankPath, c.handleYagoRankAction)
	c.mux.HandleFunc("GET "+backupPath, c.handleBackup)
}

func (c *Console) registerStaticSectionRoutes() {
	for _, item := range navItems {
		if dynamicSection(item.Path) {
			continue
		}
		c.mux.HandleFunc("GET "+item.Path, c.sectionHandler(item.Path))
	}
}
