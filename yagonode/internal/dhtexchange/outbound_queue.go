package dhtexchange

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/indextransfer"
)

const (
	MaxChunkPostings         = 1000
	DefaultMinimumPeerAgeDay = 3
)

type URLDirectory interface {
	MissingURLs(ctx context.Context, hashes []yagomodel.Hash) ([]yagomodel.Hash, error)
}

type EnqueueConfig struct {
	Redundancy         int
	PartitionExponent  int
	MinimumPeerAgeDays int
	Now                time.Time
}

type EnqueueReceipt struct {
	AcceptedPostings int
	MissingURL       int
	BadPostings      int
	TargetCopies     int
	OverflowCopies   int
	TouchedChunks    int
	acceptedRows     []yagomodel.RWIPosting
}

type OutboundChunk struct {
	Peer     yagomodel.Seed
	Postings []yagomodel.RWIPosting
}

type OutboundQueue struct {
	chunks map[yagomodel.Hash]*OutboundChunk
}

type acceptedPosting struct {
	entry yagomodel.RWIPosting
	url   yagomodel.Hash
}

func NewOutboundQueue() *OutboundQueue {
	return &OutboundQueue{}
}

func (q *OutboundQueue) EnqueueWord(
	ctx context.Context,
	urls URLDirectory,
	peers []yagomodel.Seed,
	word yagomodel.WordPostings,
	config EnqueueConfig,
) (EnqueueReceipt, error) {
	accepted, receipt, err := acceptedPostings(ctx, urls, word.Postings)
	if err != nil {
		return EnqueueReceipt{}, err
	}
	receipt.acceptedRows = acceptedRows(accepted)

	touched := make(map[yagomodel.Hash]struct{})
	partitions, err := partitionPostings(accepted, config.PartitionExponent)
	if err != nil {
		return EnqueueReceipt{}, fmt.Errorf("partition postings: %w", err)
	}
	for vertical, postings := range partitions {
		position, err := yagomodel.VerticalPosition(
			word.WordHash,
			vertical,
			config.PartitionExponent,
		)
		if err != nil {
			return EnqueueReceipt{}, fmt.Errorf("dht vertical position: %w", err)
		}

		targets, _ := indextransfer.SelectDHTTargetsAtPosition(
			position,
			peers,
			config.dhtTargets(),
		)

		for _, target := range targets {
			added := q.add(target.Peer, postings)
			receipt.TargetCopies += added
			receipt.OverflowCopies += len(postings) - added
			if added > 0 {
				touched[target.Peer.Hash] = struct{}{}
			}
		}
	}
	receipt.TouchedChunks = len(touched)

	return receipt, nil
}

func (q *OutboundQueue) Len() int {
	return len(q.chunks)
}

func (q *OutboundQueue) PostingCount() int {
	count := 0
	for _, chunk := range q.chunks {
		count += len(chunk.Postings)
	}

	return count
}

func (q *OutboundQueue) DequeueLargest() (OutboundChunk, bool) {
	var selected yagomodel.Hash
	selectedCount := -1
	for hash, chunk := range q.chunks {
		count := len(chunk.Postings)
		if count > selectedCount || count == selectedCount && hash.String() < selected.String() {
			selected = hash
			selectedCount = count
		}
	}
	if selectedCount < 0 {
		return OutboundChunk{}, false
	}

	chunk := cloneChunk(*q.chunks[selected])
	delete(q.chunks, selected)

	return chunk, true
}

func acceptedPostings(
	ctx context.Context,
	urls URLDirectory,
	postings []yagomodel.RWIPosting,
) ([]acceptedPosting, EnqueueReceipt, error) {
	candidates, receipt := postingCandidates(postings)
	if len(candidates) == 0 {
		return nil, receipt, nil
	}

	hashes := make([]yagomodel.Hash, 0, len(candidates))
	for _, candidate := range candidates {
		hashes = append(hashes, candidate.url)
	}

	missing, err := urls.MissingURLs(ctx, hashes)
	if err != nil {
		return nil, EnqueueReceipt{}, fmt.Errorf("missing urls: %w", err)
	}

	missingSet := make(map[yagomodel.Hash]struct{}, len(missing))
	for _, hash := range missing {
		missingSet[hash] = struct{}{}
	}

	accepted := make([]acceptedPosting, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := missingSet[candidate.url]; ok {
			receipt.MissingURL++
			continue
		}
		receipt.AcceptedPostings++
		accepted = append(accepted, candidate)
	}

	return accepted, receipt, nil
}

func postingCandidates(postings []yagomodel.RWIPosting) ([]acceptedPosting, EnqueueReceipt) {
	candidates := make([]acceptedPosting, 0, len(postings))
	var receipt EnqueueReceipt
	for _, posting := range postings {
		url, err := posting.URLHash()
		if err != nil {
			receipt.BadPostings++
			continue
		}
		candidates = append(candidates, acceptedPosting{entry: posting, url: url.Hash()})
	}

	return candidates, receipt
}

func acceptedRows(postings []acceptedPosting) []yagomodel.RWIPosting {
	rows := make([]yagomodel.RWIPosting, 0, len(postings))
	for _, posting := range postings {
		rows = append(rows, posting.entry)
	}

	return rows
}

func partitionPostings(
	postings []acceptedPosting,
	exponent int,
) (map[uint64][]yagomodel.RWIPosting, error) {
	partitions := make(map[uint64][]yagomodel.RWIPosting)
	for _, posting := range postings {
		vertical, err := yagomodel.VerticalPartition(posting.url, exponent)
		if err != nil {
			return nil, fmt.Errorf("dht vertical partition: %w", err)
		}
		partitions[vertical] = append(partitions[vertical], posting.entry)
	}

	return partitions, nil
}

func (c EnqueueConfig) dhtTargets() indextransfer.DHTTargetConfig {
	age := c.MinimumPeerAgeDays
	if age == 0 {
		age = DefaultMinimumPeerAgeDay
	}

	return indextransfer.DHTTargetConfig{
		Redundancy:     c.Redundancy,
		MinimumAgeDays: age,
		Now:            c.Now,
	}
}

func (q *OutboundQueue) add(peer yagomodel.Seed, postings []yagomodel.RWIPosting) int {
	if len(postings) == 0 {
		return 0
	}
	q.ensure()

	chunk, ok := q.chunks[peer.Hash]
	if !ok {
		chunk = &OutboundChunk{Peer: peer}
		q.chunks[peer.Hash] = chunk
	}

	remaining := MaxChunkPostings - len(chunk.Postings)
	if remaining <= 0 {
		return 0
	}

	added := min(remaining, len(postings))
	chunk.Postings = append(chunk.Postings, postings[:added]...)

	return added
}

func (q *OutboundQueue) ensure() {
	if q.chunks == nil {
		q.chunks = make(map[yagomodel.Hash]*OutboundChunk)
	}
}

func cloneChunk(chunk OutboundChunk) OutboundChunk {
	chunk.Postings = slices.Clone(chunk.Postings)

	return chunk
}
