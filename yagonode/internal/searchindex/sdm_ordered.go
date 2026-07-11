package searchindex

func orderedProximity(text string, terms []string) float64 {
	words := distinctWords(terms)
	if len(words) < 2 {
		return 0
	}
	positions := wordPositions(text, words)
	satisfied := 0
	for index := 0; index+1 < len(words); index++ {
		if orderedAdjacent(positions[words[index]], positions[words[index+1]]) {
			satisfied++
		}
	}

	return float64(satisfied) / float64(len(words)-1)
}

func orderedAdjacent(left []int, right []int) bool {
	leftIndex := 0
	rightIndex := 0
	for leftIndex < len(left) && rightIndex < len(right) {
		difference := right[rightIndex] - left[leftIndex]
		switch {
		case difference == 1:
			return true
		case difference <= 0:
			rightIndex++
		default:
			leftIndex++
		}
	}

	return false
}
