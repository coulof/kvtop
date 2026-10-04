package diagnose

// Default threshold constants for the deterministic rule engine.
const (
	DefaultCPUSaturationRatio    = 0.90 // 90% cores used / vCPUs allotted
	DefaultMemGuestPressureRatio = 0.90 // 90% guest RAM used from balloon stats
	DefaultDiskLatencyMs         = 50.0 // 50ms average I/O latency
	DefaultNodeOvercommitRatio   = 1.50 // 150% allocated VM memory over physical allocatable
	DefaultNodeImbalanceRatio    = 2.00 // 2.0x cluster median
	DefaultStaleThresholdSec     = 10.0 // 10s without new samples
)

// Thresholds holds configurable threshold values used during diagnosis.
type Thresholds struct {
	CPUSaturationRatio    float64 `json:"cpu_saturation_ratio"`
	MemGuestPressureRatio float64 `json:"mem_guest_pressure_ratio"`
	DiskLatencyMs         float64 `json:"disk_latency_ms"`
	NodeOvercommitRatio   float64 `json:"node_overcommit_ratio"`
	NodeImbalanceRatio    float64 `json:"node_imbalance_ratio"`
	StaleThresholdSec     float64 `json:"stale_threshold_seconds"`
}

// DefaultThresholds returns a Thresholds struct with standard defaults.
func DefaultThresholds() Thresholds {
	return Thresholds{
		CPUSaturationRatio:    DefaultCPUSaturationRatio,
		MemGuestPressureRatio: DefaultMemGuestPressureRatio,
		DiskLatencyMs:         DefaultDiskLatencyMs,
		NodeOvercommitRatio:   DefaultNodeOvercommitRatio,
		NodeImbalanceRatio:    DefaultNodeImbalanceRatio,
		StaleThresholdSec:     DefaultStaleThresholdSec,
	}
}
