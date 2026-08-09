package adminui

type ProcessMemory struct {
	ResidentBytes          int64
	ResidentAvailable      bool
	AnonymousBytes         int64
	AnonymousAvailable     bool
	FileBackedBytes        int64
	FileBackedAvailable    bool
	SharedMemoryBytes      int64
	SharedMemoryAvailable  bool
	GoHeapObjectsBytes     int64
	GoHeapObjectsAvailable bool
}
