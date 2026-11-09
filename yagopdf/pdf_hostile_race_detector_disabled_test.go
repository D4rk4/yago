//go:build !race

package yagopdf

const (
	raceDetectorSlowdown = 1
	allocationMeasured   = true
)
