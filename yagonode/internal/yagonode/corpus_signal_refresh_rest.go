package yagonode

import "time"

const corpusSignalRestPerScanTime = 9

func corpusSignalRefreshRest(scanTime time.Duration) time.Duration {
	return max(defaultCorpusSignalRefreshInterval, scanTime*corpusSignalRestPerScanTime)
}
