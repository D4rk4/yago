package websearch

import "fmt"

func (race *engineRace) completedResults() ([]Result, error) {
	select {
	case attempt := <-race.attempts:
		if results := race.evaluate(race.drainReady(attempt)); len(results) > 0 {
			return results, nil
		}
	default:
	}
	return nil, fmt.Errorf("web-search engine race: %w", race.ctx.Err())
}
