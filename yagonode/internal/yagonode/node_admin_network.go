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
		Available:       true,
		DHTOpen:         report.Open,
		PublicReachable: report.State.PublicReachable,
		BlockingReason:  report.BlockingReason,
		Gates:           adminNetworkGates(report.Gates),
		Seedlists:       s.adminSeedlists(ctx),
	}
	if s.self != nil {
		status.OwnFlags = advertisedFlagStates(s.self.SelfSeed(ctx))
	}

	if s.roster != nil {
		status.KnownPeers = s.roster.KnownPeerCount(ctx)
		status.ReachablePeers = s.roster.ReachablePeerCount(ctx)
		status.Peers = s.adminNetworkPeers(ctx, s.roster.FreshestPeers(ctx, status.KnownPeers))
	}

	return status
}

// blockedSet loads the current blocklist once as a set for marking the peer
// table. A read error degrades to no marks rather than hiding the table.
func (s networkSource) blockedSet(ctx context.Context) map[yagomodel.Hash]struct{} {
	if s.blocks == nil {
		return nil
	}
	entries, err := s.blocks.Blocked(ctx)
	if err != nil {
		slog.WarnContext(ctx, "read peer blocklist for admin table failed", slog.Any("error", err))

		return nil
	}
	set := make(map[yagomodel.Hash]struct{}, len(entries))
	for _, entry := range entries {
		set[entry.Hash] = struct{}{}
	}

	return set
}

func (s networkSource) adminSeedlists(ctx context.Context) []adminui.SeedlistEntry {
	entries := make([]adminui.SeedlistEntry, 0, len(s.seedlistURLs))
	for _, url := range s.seedlistURLs {
		entry := adminui.SeedlistEntry{URL: url}
		if s.status != nil {
			if st, found, err := s.status.Get(ctx, url); err == nil && found {
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
	blocked := s.blockedSet(ctx)
	peers := make([]adminui.NetworkPeer, 0, len(seeds))
	for _, seed := range seeds {
		name, _ := seed.Name.Get()
		address, _ := seed.NetworkAddress()
		peerType, _ := seed.PeerType.Get()
		rwiCount, _ := seed.RWICount.Get()
		_, isBlocked := blocked[seed.Hash]
		lastSeen, seen := seed.LastSeen.Get()
		uptime, _ := seed.Uptime.Get()
		health := adminui.SwarmHealthScore(
			lastSeen.Time(), seen, uptime, seed.AgeDays(now), now,
		)
		var lastSeenAt time.Time
		if seen {
			lastSeenAt = lastSeen.Time()
		}
		peers = append(peers, adminui.NetworkPeer{
			Name:       name,
			Hash:       string(seed.Hash),
			Address:    address,
			Type:       string(peerType),
			Flags:      seedFlagLabels(seed),
			RWICount:   rwiCount,
			LastSeen:   seedLastSeen(seed),
			LastSeenAt: lastSeenAt,
			AgeDays:    seed.AgeDays(now),
			Blocked:    isBlocked,
			Health:     health,
			HealthTag:  adminui.SwarmHealthTag(health),
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
) (adminui.PeerDetail, bool) {
	parsed, err := yagomodel.ParseHash(hash)
	if err != nil {
		return adminui.PeerDetail{}, false
	}
	seed, ok := s.roster.PeerByHash(ctx, parsed)
	if !ok {
		return adminui.PeerDetail{}, false
	}

	detail := peerDetailFromSeed(seed, s.now())
	if s.blocks != nil {
		if blocked, err := s.blocks.IsBlocked(ctx, parsed); err == nil {
			detail.Blocked = blocked
		}
	}

	return detail, true
}

func peerDetailFromSeed(seed yagomodel.Seed, now time.Time) adminui.PeerDetail {
	name, _ := seed.Name.Get()
	address, _ := seed.NetworkAddress()
	peerType, _ := seed.PeerType.Get()
	rwi, _ := seed.RWICount.Get()
	urls, _ := seed.URLCount.Get()
	knownSeeds, _ := seed.KnownSeedCount.Get()
	uptime, _ := seed.Uptime.Get()
	sentWords, _ := seed.SentWordCount.Get()
	receivedWords, _ := seed.ReceivedWordCount.Get()
	sentURLs, _ := seed.SentURLCount.Get()
	receivedURLs, _ := seed.ReceivedURLCount.Get()

	version := ""
	if v, ok := seed.Version.Get(); ok {
		version = v.String()
	}

	return adminui.PeerDetail{
		Name:          name,
		Hash:          string(seed.Hash),
		Address:       address,
		Version:       version,
		Type:          string(peerType),
		Flags:         seedFlagLabels(seed),
		LastSeen:      seedLastSeen(seed),
		AgeDays:       seed.AgeDays(now),
		UptimeMinutes: uptime,
		RWIWords:      rwi,
		URLs:          urls,
		KnownSeeds:    knownSeeds,
		SentWords:     sentWords,
		ReceivedWords: receivedWords,
		SentURLs:      sentURLs,
		ReceivedURLs:  receivedURLs,
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

func seedLastSeen(seed yagomodel.Seed) string {
	last, ok := seed.LastSeen.Get()
	if !ok {
		return ""
	}

	return last.Time().UTC().Format(time.RFC3339)
}
