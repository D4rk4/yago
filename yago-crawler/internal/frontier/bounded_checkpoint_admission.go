package frontier

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type boundedAdmissionWindow struct {
	visited  []bool
	pages    map[string]int
	retired  map[string]struct{}
	accepted map[string]struct{}
}

type boundedAdmissionCandidate struct {
	page     frontierCandidate
	position int
}

func (f *Frontier) loadBoundedAdmissionState(
	ctx context.Context,
	run *crawlRun,
	candidates []frontierCandidate,
) (frontiercheckpoint.AdmissionBatchState, error) {
	if run == nil || !run.boundedRecovery || len(candidates) == 0 {
		return frontiercheckpoint.AdmissionBatchState{}, nil
	}
	checkpoint := f.checkpoint.(boundedRecoveryCheckpoint)
	pages := make([]frontiercheckpoint.Page, 0, len(candidates))
	for _, candidate := range candidates {
		pages = append(pages, checkpointPage(candidate))
	}

	state, err := checkpoint.AdmissionBatchState(
		context.WithoutCancel(ctx),
		run.provenanceValue,
		pages,
	)
	if err != nil {
		return frontiercheckpoint.AdmissionBatchState{}, fmt.Errorf(
			"load bounded frontier admission state: %w",
			err,
		)
	}

	return state, nil
}

func newBoundedAdmissionWindow(
	run *crawlRun,
	state frontiercheckpoint.AdmissionBatchState,
	candidates []frontierCandidate,
) (boundedAdmissionWindow, error) {
	if run == nil || !run.boundedRecovery {
		return boundedAdmissionWindow{}, nil
	}
	if len(state.Visited) != len(candidates) {
		return boundedAdmissionWindow{}, fmt.Errorf(
			"%w: bounded admission result length changed",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	window := boundedAdmissionWindow{
		visited:  state.Visited,
		pages:    make(map[string]int, len(state.HostStates)),
		retired:  make(map[string]struct{}),
		accepted: make(map[string]struct{}, len(candidates)),
	}
	for _, candidate := range candidates {
		if _, found := run.profiles[candidate.profileHandle]; !found {
			return boundedAdmissionWindow{}, fmt.Errorf(
				"%w: bounded admission crawl profile changed",
				frontiercheckpoint.ErrCorruptCheckpoint,
			)
		}
		host, found := state.HostStates[candidate.host]
		if !found || candidate.host == "" || host.Pages > uint64(math.MaxInt) {
			return boundedAdmissionWindow{}, fmt.Errorf(
				"%w: bounded admission host state is invalid",
				frontiercheckpoint.ErrCorruptCheckpoint,
			)
		}
		pages, _ := platformPageTotal(host.Pages)
		window.pages[candidate.host] = pages
		if host.Retired {
			window.retired[candidate.host] = struct{}{}
		}
	}

	return window, nil
}

func (f *Frontier) acceptWithAdmissionWindowLocked(
	ctx context.Context,
	runID uuid.UUID,
	run *crawlRun,
	admission boundedAdmissionCandidate,
	window *boundedAdmissionWindow,
) (bool, bool) {
	candidate := admission.page
	if run == nil || !run.boundedRecovery {
		return f.acceptLocked(ctx, runID, candidate)
	}
	if _, cancelled := f.state.cancelled[string(candidate.provenance)]; cancelled {
		return false, false
	}
	if _, retired := window.retired[candidate.host]; retired {
		return false, false
	}
	if _, retired := run.retiredHosts[candidate.host]; retired {
		return false, false
	}
	profile := run.profiles[candidate.profileHandle]
	if window.visited[admission.position] {
		return false, true
	}
	if _, duplicate := window.accepted[candidate.normURL]; duplicate {
		return false, true
	}
	if profile.Profile.MaxPagesPerHost != yagocrawlcontract.UnlimitedPagesPerHost &&
		window.pages[candidate.host] >= profile.Profile.MaxPagesPerHost {
		return false, false
	}
	if run.maxPages > 0 && run.pages >= run.maxPages {
		if !run.budgetExceeded {
			run.budgetExceeded = true
			slog.WarnContext(ctx, msgRunPageBudgetReached,
				slog.String("runId", runID.String()),
				slog.Int("maxPagesPerRun", run.maxPages),
			)
		}

		return false, false
	}
	window.accepted[candidate.normURL] = struct{}{}
	window.pages[candidate.host]++
	run.pages++
	f.state.completion.Track(runID)

	return true, false
}
