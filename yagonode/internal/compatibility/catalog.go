package compatibility

import "slices"

type State string

const (
	Implemented State = "implemented"
	Partial     State = "partial"
	Planned     State = "planned"
	Unsupported State = "unsupported"
)

const (
	areaYaCyPeerProtocol    = "YaCy peer protocol"
	areaSearchCompatibility = "Search compatibility"
	areaAgentAPI            = "Agent API compatibility"
	areaAdminOperations     = "Admin and operations"
)

type Surface struct {
	Area     string   `json:"area"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Methods  []string `json:"methods"`
	State    State    `json:"state"`
	Behavior string   `json:"behavior"`
	Evidence []string `json:"evidence"`
	Notes    string   `json:"notes"`
}

type Count struct {
	State State `json:"state"`
	Total int   `json:"total"`
}

type Report struct {
	Surfaces []Surface `json:"surfaces"`
	Counts   []Count   `json:"counts"`
}

type surfaceSpec struct {
	Name     string
	Path     string
	Methods  []string
	State    State
	Behavior string
	Evidence []string
	Notes    string
}

func Catalog() Report {
	surfaces := catalogSurfaces()

	return Report{
		Surfaces: cloneSurfaces(surfaces),
		Counts:   counts(surfaces),
	}
}

func catalogSurfaces() []Surface {
	surfaces := make([]Surface, 0, catalogSurfaceCount())
	surfaces = append(surfaces, surfacesFor(areaYaCyPeerProtocol, yacyPeerSurfaceSpecs)...)
	surfaces = append(surfaces, surfacesFor(areaSearchCompatibility, searchSurfaceSpecs)...)
	surfaces = append(surfaces, surfacesFor(areaAgentAPI, agentAPISurfaceSpecs)...)
	surfaces = append(surfaces, surfacesFor(areaAdminOperations, adminSurfaceSpecs)...)

	return surfaces
}

func catalogSurfaceCount() int {
	return len(yacyPeerSurfaceSpecs) +
		len(searchSurfaceSpecs) +
		len(agentAPISurfaceSpecs) +
		len(adminSurfaceSpecs)
}

func surfacesFor(area string, specs []surfaceSpec) []Surface {
	out := make([]Surface, len(specs))
	for i, spec := range specs {
		out[i] = surfaceFor(area, spec)
	}

	return out
}

func surfaceFor(area string, spec surfaceSpec) Surface {
	return Surface{
		Area:     area,
		Name:     spec.Name,
		Path:     spec.Path,
		Methods:  slices.Clone(spec.Methods),
		State:    spec.State,
		Behavior: spec.Behavior,
		Evidence: slices.Clone(spec.Evidence),
		Notes:    spec.Notes,
	}
}

func cloneSurfaces(surfaces []Surface) []Surface {
	out := slices.Clone(surfaces)
	for i := range out {
		out[i].Methods = slices.Clone(out[i].Methods)
		out[i].Evidence = slices.Clone(out[i].Evidence)
	}

	return out
}

func counts(surfaces []Surface) []Count {
	totals := map[State]int{}
	for _, surface := range surfaces {
		totals[surface.State]++
	}

	states := []State{Implemented, Partial, Planned, Unsupported}
	out := make([]Count, 0, len(states))
	for _, state := range states {
		if total := totals[state]; total > 0 {
			out = append(out, Count{State: state, Total: total})
		}
	}

	return out
}
