package adminui

import "fmt"

type StoragePressureStatus struct {
	AvailableBytes          uint64
	ReservedFreeBytes       uint64
	PressureHysteresisBytes uint64
	MeasurementAvailable    bool
	Pressured               bool
}

type StoragePressureStatusSource interface {
	StoragePressureStatus() StoragePressureStatus
}

func applyIndexNodeStorageStatus(
	view indexStorageStatus,
	source StoragePressureStatusSource,
) indexStorageStatus {
	if source == nil {
		return view
	}
	status := source.StoragePressureStatus()
	view.NodeVisible = true
	view.NodePressured = status.Pressured
	if !status.MeasurementAvailable {
		view.NodeText = "Measurement unavailable · gate-managed crawl/index ingestion paused · free filesystem space or lower reserve and recovery margin; bbolt deletion may only create reusable internal pages"

		return view
	}
	view.NodeAvailable = true
	view.NodeText = storagePressureBytes(status.AvailableBytes) +
		" available · reserve " + storagePressureBytes(status.ReservedFreeBytes)
	if status.PressureHysteresisBytes > 0 {
		view.NodeText += " + " +
			storagePressureBytes(status.PressureHysteresisBytes) + " recovery margin"
	}
	if status.Pressured {
		view.NodeText += " · gate-managed crawl/index ingestion paused · free filesystem space or lower reserve and recovery margin; bbolt deletion may only create reusable internal pages"
	}

	return view
}

func storagePressureBytes(bytes uint64) string {
	if bytes == 0 {
		return "0 B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}

	return fmt.Sprintf("%.1f %s", value, units[unit])
}
