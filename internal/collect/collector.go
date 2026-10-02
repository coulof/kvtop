package collect

import (
	"context"
	"time"
)

// VMISample represents a raw snapshot of metrics for one VirtualMachineInstance at a point in time.
type VMISample struct {
	Namespace string
	Name      string
	Node      string
	Timestamp time.Time

	// CPU
	CPUUsageSeconds float64

	// Memory
	HasBalloonStats       bool
	MemoryDomainBytes     uint64
	MemoryUsableBytes     uint64
	MemoryAvailableBytes  uint64
	MemoryUnusedBytes     uint64
	MemoryResidentBytes   uint64

	// Network counters (sum across interfaces)
	NetRxBytesTotal uint64
	NetTxBytesTotal uint64

	// Storage counters (sum across drives)
	StorageReadIOPSTotal   uint64
	StorageWriteIOPSTotal  uint64
	StorageReadBytesTotal  uint64
	StorageWriteBytesTotal uint64
}

// Collector defines the interface for periodic metric collectors.
type Collector interface {
	Name() string
	Collect(ctx context.Context) ([]VMISample, error)
}
