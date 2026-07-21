package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/seedimport"
)

// seedImportStatusReader reads the durable last-import status per seed-list URL.
// A nil reader leaves the seedlist table without import history.
type seedImportStatusReader interface {
	Get(ctx context.Context, url string) (seedimport.Status, bool, error)
}

// selfSeedSource supplies the seed this node currently advertises to the swarm;
// nodestatus.Report satisfies it.
type selfSeedSource interface {
	SelfSeed(ctx context.Context) yagomodel.Seed
}

type networkSource struct {
	gates        dhtGateStatusSource
	roster       peerroster.Roster
	seedlistURLs []string
	status       seedImportStatusReader
	blocks       peerBlockStore
	self         selfSeedSource
	now          func() time.Time
}

func newNetworkSource(
	gates dhtGateStatusSource,
	roster peerroster.Roster,
	seedlistURLs []string,
	status seedImportStatusReader,
	blocks peerBlockStore,
) networkSource {
	return networkSource{
		gates:        gates,
		roster:       roster,
		seedlistURLs: seedlistURLs,
		status:       status,
		blocks:       blocks,
		now:          time.Now,
	}
}

// withSelf attaches the advertised-seed provider so the Network section can show
// the capability flags this node publishes to the swarm. A nil provider hides the
// flags block.
func (s networkSource) withSelf(self selfSeedSource) networkSource {
	s.self = self

	return s
}

func (s networkSource) Network(ctx context.Context) adminui.NetworkStatus {
	report := s.gates.response(ctx)
	status := adminui.NetworkStatus{
		Available:                    true,
		DHTOpen:                      report.Open,
		PublicReachable:              report.State.PublicReachable,
		PublicReachabilityKnown:      report.reachability.state != publicReachabilityUnknown,
		PublicReachabilitySource:     adminReachabilitySource(report.reachability.source),
		PublicReachabilityObservedAt: adminReachabilityObservedAt(report.reachability.observedAt),
		BlockingReason:               report.BlockingReason,
		Gates:                        adminNetworkGates(report.Gates),
		Seedlists:                    s.adminSeedlists(ctx),
	}
	if s.self != nil {
		status.OwnFlags = advertisedFlagStates(s.self.SelfSeed(ctx))
	}

	if s.roster != nil {
		roster := s.peerRosterSnapshot(ctx)
		status.RosterAvailable = roster.available
		status.KnownPeers = roster.knownPeers
		status.ReachablePeers = roster.reachablePeers
		status.Peers = s.adminNetworkPeers(ctx, roster.seeds)
	}

	return status
}

func adminReachabilityObservedAt(observedAt time.Time) string {
	if observedAt.IsZero() {
		return ""
	}

	return observedAt.UTC().Format(time.RFC3339)
}

func adminReachabilitySource(source publicReachabilitySource) string {
	switch source {
	case publicReachabilitySourcePeerBackPing:
		return adminui.PublicReachabilityPeerBackPing
	case publicReachabilitySourcePinnedProbe:
		return adminui.PublicReachabilityPinnedProbe
	case publicReachabilitySourceDerivedProbe:
		return adminui.PublicReachabilityDerivedProbe
	default:
		return ""
	}
}

func (s networkSource) blockedSet(ctx context.Context) (map[yagomodel.Hash]struct{}, bool) {
	if s.blocks == nil {
		return nil, true
	}
	entries, err := s.blocks.Blocked(ctx)
	if err != nil {
		slog.WarnContext(ctx, "read peer blocklist for admin table failed", slog.Any("error", err))

		return nil, false
	}
	set := make(map[yagomodel.Hash]struct{}, len(entries))
	for _, entry := range entries {
		set[entry.Hash] = struct{}{}
	}

	return set, true
}

func (s networkSource) adminSeedlists(ctx context.Context) []adminui.SeedlistEntry {
	entries := make([]adminui.SeedlistEntry, 0, len(s.seedlistURLs))
	for _, url := range s.seedlistURLs {
		entry := adminui.SeedlistEntry{URL: url}
		if s.status != nil {
			st, found, err := s.status.Get(ctx, url)
			if err == nil {
				entry.StatusKnown = true
			}
			if err == nil && found {
				entry.Imported = true
				entry.OK = st.OK
				entry.LastImport = st.LastImport.UTC().Format(time.RFC3339)
				entry.Result = seedlistResult(st)
			}
		}
		entries = append(entries, entry)
	}

	return entries
}

func seedlistResult(status seedimport.Status) string {
	if status.OK {
		return fmt.Sprintf("%d seeds", status.Seeds)
	}
	if status.Error != "" {
		return "failed: " + status.Error
	}

	return "failed"
}

func adminNetworkGates(results []dhtGateResultResponse) []adminui.NetworkGate {
	gates := make([]adminui.NetworkGate, 0, len(results))
	for _, result := range results {
		gates = append(gates, adminui.NetworkGate{
			Name:   result.Name,
			Open:   result.Open,
			Reason: result.Reason,
		})
	}

	return gates
}

func (s networkSource) adminNetworkPeers(
	ctx context.Context,
	seeds []yagomodel.Seed,
) []adminui.NetworkPeer {
	now := s.now()
	blocked, blockStatusKnown := s.blockedSet(ctx)
	peers := make([]adminui.NetworkPeer, 0, len(seeds))
	for _, seed := range seeds {
		name, _ := seed.Name.Get()
		address, _ := seed.NetworkAddress()
		peerType, _ := seed.PeerType.Get()
		rwiCount, rwiKnown := seedStatistic(seed.RWICount)
		_, isBlocked := blocked[seed.Hash]
		lastSeen, lastSeenKnown := seedLastSeenStatistic(seed, now)
		uptime, uptimeKnown := seedStatistic(seed.Uptime)
		ageDays, ageKnown := seedAgeStatistic(seed, now)
		healthKnown := lastSeenKnown && uptimeKnown && ageKnown
		health := 0
		if healthKnown {
			health = adminui.SwarmHealthScore(lastSeen, true, uptime, ageDays, now)
		}
		var lastSeenAt time.Time
		if lastSeenKnown {
			lastSeenAt = lastSeen
		}
		peers = append(peers, adminui.NetworkPeer{
			Name:             name,
			Hash:             string(seed.Hash),
			Address:          address,
			Type:             string(peerType),
			Flags:            seedFlagLabels(seed),
			RWICount:         rwiCount,
			RWIKnown:         rwiKnown,
			LastSeen:         seedLastSeen(seed, now),
			LastSeenAt:       lastSeenAt,
			AgeDays:          ageDays,
			AgeKnown:         ageKnown,
			Blocked:          isBlocked,
			BlockStatusKnown: blockStatusKnown,
			Health:           health,
			HealthTag:        adminui.SwarmHealthTag(health),
			HealthKnown:      healthKnown,
		})
	}

	return peers
}

// peerDetailSource resolves a peer hash to its published profile for the console's
// per-peer detail page. It reads through the roster and carries no secrets.
type peerDetailSource struct {
	roster peerroster.Roster
	blocks peerBlockStore
	now    func() time.Time
}

func newPeerDetailSource(roster peerroster.Roster, blocks peerBlockStore) peerDetailSource {
	return peerDetailSource{roster: roster, blocks: blocks, now: time.Now}
}

func (s peerDetailSource) PeerDetail(
	ctx context.Context,
	hash string,
) (adminui.PeerDetail, bool, error) {
	parsed, valid := parseAdminPeerHash(hash)
	if !valid {
		return adminui.PeerDetail{}, false, nil
	}
	if s.roster == nil {
		return adminui.PeerDetail{}, false, nil
	}
	seed, ok, err := readAdminPeer(ctx, s.roster, parsed)
	if err != nil {
		return adminui.PeerDetail{}, false, fmt.Errorf("read admin peer: %w", err)
	}
	if !ok {
		return adminui.PeerDetail{}, false, nil
	}

	detail := peerDetailFromSeed(seed, s.now())
	detail.BlockStatusKnown = true
	if s.blocks != nil {
		if blocked, err := s.blocks.IsBlocked(ctx, parsed); err == nil {
			detail.Blocked = blocked
		} else {
			detail.BlockStatusKnown = false
		}
	}

	return detail, true, nil
}

func peerDetailFromSeed(seed yagomodel.Seed, now time.Time) adminui.PeerDetail {
	name, _ := seed.Name.Get()
	address, _ := seed.NetworkAddress()
	peerType, _ := seed.PeerType.Get()
	rwi, rwiKnown := seedStatistic(seed.RWICount)
	urls, urlsKnown := seedStatistic(seed.URLCount)
	knownSeeds, knownSeedsKnown := seedStatistic(seed.KnownSeedCount)
	uptime, uptimeKnown := seedStatistic(seed.Uptime)
	sentWords, sentWordsKnown := seedTransferStatistic(seed.SentWordCount)
	receivedWords, receivedWordsKnown := seedTransferStatistic(seed.ReceivedWordCount)
	sentURLs, sentURLsKnown := seedTransferStatistic(seed.SentURLCount)
	receivedURLs, receivedURLsKnown := seedTransferStatistic(seed.ReceivedURLCount)
	ageDays, ageKnown := seedAgeStatistic(seed, now)

	version := ""
	if v, ok := seed.Version.Get(); ok {
		version = v.String()
	}

	return adminui.PeerDetail{
		Name:               name,
		Hash:               string(seed.Hash),
		Address:            address,
		Version:            version,
		Type:               string(peerType),
		Flags:              seedFlagLabels(seed),
		LastSeen:           seedLastSeen(seed, now),
		AgeDays:            ageDays,
		AgeKnown:           ageKnown,
		UptimeMinutes:      uptime,
		UptimeKnown:        uptimeKnown,
		RWIWords:           rwi,
		RWIWordsKnown:      rwiKnown,
		URLs:               urls,
		URLsKnown:          urlsKnown,
		KnownSeeds:         knownSeeds,
		KnownSeedsKnown:    knownSeedsKnown,
		SentWords:          sentWords,
		SentWordsKnown:     sentWordsKnown,
		ReceivedWords:      receivedWords,
		ReceivedWordsKnown: receivedWordsKnown,
		SentURLs:           sentURLs,
		SentURLsKnown:      sentURLsKnown,
		ReceivedURLs:       receivedURLs,
		ReceivedURLsKnown:  receivedURLsKnown,
	}
}

var seedFlagBits = []struct {
	bit   int
	label string
}{
	{yagomodel.FlagDirectConnect, "direct"},
	{yagomodel.FlagAcceptRemoteCrawl, "remote-crawl"},
	{yagomodel.FlagAcceptRemoteIndex, "remote-index"},
	{yagomodel.FlagRootNode, "root"},
	{yagomodel.FlagSSLAvailable, "ssl"},
}

// advertisedFlagStates reports every known capability flag with the state the
// given seed advertises, so the console shows set and unset bits alike.
func advertisedFlagStates(seed yagomodel.Seed) []adminui.NetworkFlag {
	flags, ok := seed.Flags.Get()
	if !ok {
		return nil
	}
	states := make([]adminui.NetworkFlag, 0, len(seedFlagBits))
	for _, entry := range seedFlagBits {
		states = append(states, adminui.NetworkFlag{
			Name: entry.label,
			Set:  flags.Get(entry.bit),
		})
	}

	return states
}

func seedFlagLabels(seed yagomodel.Seed) []string {
	flags, ok := seed.Flags.Get()
	if !ok {
		return nil
	}
	labels := make([]string, 0, len(seedFlagBits))
	for _, entry := range seedFlagBits {
		if flags.Get(entry.bit) {
			labels = append(labels, entry.label)
		}
	}

	return labels
}

func seedLastSeen(seed yagomodel.Seed, now time.Time) string {
	last, ok := seedLastSeenStatistic(seed, now)
	if !ok {
		return ""
	}

	return last.UTC().Format(time.RFC3339)
}
