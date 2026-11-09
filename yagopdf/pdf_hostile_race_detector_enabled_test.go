//go:build race

package yagopdf

const (
	raceDetectorSlowdown = 10
	allocationMeasured   = false
)
