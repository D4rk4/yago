package yagonode

func buildSurfaceDHT(in assembleSurfacesInput, runtime crawlProcess) dhtOutboundProcess {
	return buildRuntimeDHTOutbound(dhtOutboundRuntimeAssembly{
		ctx:         in.ctx,
		config:      in.config,
		storage:     in.vault,
		nodeStorage: in.storage,
		report:      in.report,
		roster:      in.roster,
		client:      in.peerClient,
		observer: tallyOutboundObserver{
			next:  in.telemetry.dhtOutbound,
			tally: in.tally,
		},
		events: in.telemetry.recorder,
		crawl:  crawlQueueDepthSource{probe: crawlQueueProbe(runtime)},
		index:  indexQueueDepthSource{probe: indexQueueProbe(runtime)},
	})
}
