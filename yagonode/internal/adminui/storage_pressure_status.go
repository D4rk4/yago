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

func applyStoragePressureStatus(
	view systemMonitorView,
	source StoragePressureStatusSource,
) systemMonitorView {
	if source == nil {
		return view
	}
	status := source.StoragePressureStatus()
	view.NodeStoragePressureVisible = true
	view.NodeStoragePressured = status.Pressured
	if !status.MeasurementAvailable {
		view.NodeStoragePressureText = "Measurement unavailable · gate-managed crawl/index ingestion paused · free filesystem space or lower reserve and recovery margin; bbolt deletion may only create reusable internal pages"

		return view
	}
	view.NodeStoragePressureAvailable = true
	view.NodeStoragePressureText = storagePressureBytes(status.AvailableBytes) +
		" available · reserve " + storagePressureBytes(status.ReservedFreeBytes)
	if status.PressureHysteresisBytes > 0 {
		view.NodeStoragePressureText += " + " +
			storagePressureBytes(status.PressureHysteresisBytes) + " recovery margin"
	}
	if status.Pressured {
		view.NodeStoragePressureText += " · gate-managed crawl/index ingestion paused · free filesystem space or lower reserve and recovery margin; bbolt deletion may only create reusable internal pages"
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
