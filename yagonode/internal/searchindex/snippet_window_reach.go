package searchindex

type snippetWindowReach struct {
	occurrences []snippetOccurrence
	reaching    []int
}

func newSnippetWindowReach(occurrences []snippetOccurrence) snippetWindowReach {
	return snippetWindowReach{
		occurrences: occurrences,
		reaching:    make([]int, 0, len(occurrences)),
	}
}

func (r *snippetWindowReach) include(index int) {
	for len(r.reaching) > 0 &&
		r.occurrences[r.reaching[len(r.reaching)-1]].runeEnd <= r.occurrences[index].runeEnd {
		r.reaching = r.reaching[:len(r.reaching)-1]
	}
	r.reaching = append(r.reaching, index)
}

func (r *snippetWindowReach) startAt(left int) {
	if r.reaching[0] < left {
		r.reaching = r.reaching[1:]
	}
}

func (r *snippetWindowReach) furthest() snippetOccurrence {
	return r.occurrences[r.reaching[0]]
}
