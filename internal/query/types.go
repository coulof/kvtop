package query

import (
	"errors"
	"time"

	"github.com/coulof/kvtop/internal/kube"
)

const (
	// SchemaVersion defines the stable JSON contract schema.
	SchemaVersion = "kvtop/v1"

	// Data source identifiers as specified in AGENTS.md.
	SourceVirtHandler   = "virt-handler"
	SourceMetricsServer = "metrics-server"
	SourceKubernetes    = "kubernetes"
	SourcePrometheus    = "prometheus"
	SourceVirsh         = "virsh"
)

var (
	// ErrVMNotFound is returned when a requested VM cannot be found in the store.
	ErrVMNotFound = errors.New("vm not found")

	// ErrWindowTooSmall is returned when requested window is less than the minimum 10s.
	ErrWindowTooSmall = errors.New("window must be at least 10s (virt-handler caches domain stats for ~5s)")
)

// MetricSummary provides statistical aggregates over the query window.
type MetricSummary struct {
	Min  float64 `json:"min"`
	Avg  float64 `json:"avg"`
	Max  float64 `json:"max"`
	P95  float64 `json:"p95"`
	Last float64 `json:"last"`
}

// TimestampedPoint represents a single raw time-series point.
type TimestampedPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// VMMetricSamples contains raw sample series over the window when --samples is requested.
type VMMetricSamples struct {
	CPUUsageCores        []TimestampedPoint `json:"cpu_cores_used,omitempty"`
	MemoryGuestUsedBytes []TimestampedPoint `json:"mem_guest_used_bytes,omitempty"`
	NetRxBytesPerSec     []TimestampedPoint `json:"net_rx_bytes_per_s,omitempty"`
	NetTxBytesPerSec     []TimestampedPoint `json:"net_tx_bytes_per_s,omitempty"`
	StorageTotalIOPS     []TimestampedPoint `json:"disk_iops_total,omitempty"`
}

// MigrationInfo describes active or recent migration state.
type MigrationInfo struct {
	SourceNode string `json:"source_node"`
	TargetNode string `json:"target_node"`
	Phase      string `json:"phase"`
}

// VMContext holds Kubernetes informer metadata joined onto the VM.
type VMContext struct {
	CPUTopology           kube.CPUTopology     `json:"cpu_topology"`
	DedicatedCPUPlacement bool                 `json:"dedicated_cpu_placement"`
	MemoryGuest           string               `json:"memory_guest,omitempty"`
	MemoryRequested       string               `json:"memory_requested,omitempty"`
	MemoryLimit           string               `json:"memory_limit,omitempty"`
	InstanceType          string               `json:"instance_type,omitempty"`
	Preference            string               `json:"preference,omitempty"`
	EvictionStrategy      string               `json:"eviction_strategy,omitempty"`
	Conditions            []kube.VMICondition  `json:"conditions,omitempty"`
	Volumes               []kube.VMIVolumeInfo `json:"volumes,omitempty"`
}

// VMItem represents a single VM with units explicitly named in each field.
type VMItem struct {
	Namespace               string            `json:"namespace"`
	Name                    string            `json:"name"`
	Node                    string            `json:"node"`
	Phase                   string            `json:"phase"`
	IP                      *string           `json:"ip"`
	IPReason                string            `json:"ip_reason,omitempty"`
	AllottedVCPUs           int64             `json:"allotted_vcpus"`
	CPUCoresUsed            *MetricSummary    `json:"cpu_cores_used"`
	CPUCoresUsedSource      string            `json:"cpu_cores_used_source"`
	CPUSaturationPercent    *MetricSummary    `json:"cpu_saturation_percent"`
	MemGuestUsedBytes       *MetricSummary    `json:"mem_guest_used_bytes"`
	MemGuestUsedBytesReason string            `json:"mem_guest_used_bytes_reason,omitempty"`
	MemGuestUsedBytesSource string            `json:"mem_guest_used_bytes_source,omitempty"`
	MemGuestTotalBytes      *uint64           `json:"mem_guest_total_bytes"`
	MemGuestUsedPercent     *MetricSummary    `json:"mem_guest_used_percent"`
	NetRxBytesPerSec        *MetricSummary    `json:"net_rx_bytes_per_s"`
	NetRxBytesPerSecSource  string            `json:"net_rx_bytes_per_s_source"`
	NetTxBytesPerSec        *MetricSummary    `json:"net_tx_bytes_per_s"`
	NetTxBytesPerSecSource  string            `json:"net_tx_bytes_per_s_source"`
	DiskIOPSTotal           *MetricSummary    `json:"disk_iops_total"`
	DiskIOPSTotalSource     string            `json:"disk_iops_total_source"`
	DiskLatencyMs           *MetricSummary    `json:"disk_latency_ms"`
	DiskLatencyMsReason     string            `json:"disk_latency_ms_reason,omitempty"`
	DiskLatencyMsSource     string            `json:"disk_latency_ms_source,omitempty"`
	IsMigrating             bool              `json:"is_migrating"`
	Migration               *MigrationInfo    `json:"migration,omitempty"`
	Stale                   bool              `json:"stale"`
	StaleAgeSeconds         float64           `json:"stale_age_seconds"`
	Samples                 *VMMetricSamples  `json:"samples,omitempty"`
	Context                 *VMContext        `json:"context,omitempty"`
}

// TopOptions defines filter, sort, and pagination arguments for QueryTop.
type TopOptions struct {
	SortBy         string        // "cpu", "mem", "net", "disk"
	BySaturation   bool          // If true and SortBy == "cpu", sort by saturation percent
	Namespaces     []string      // Filter by namespaces
	Node           string        // Filter by node
	Limit          int           // Limit number of items (default: 10)
	Window         time.Duration // Time window (default: 15s, min: 10s)
	IncludeSamples bool          // Include raw time-series points
}

// TopResult is the response payload for `kvtop top`.
type TopResult struct {
	Schema         string   `json:"schema"`
	CollectedAt    string   `json:"collected_at"`
	WindowSeconds  float64  `json:"window_seconds"`
	Total          int      `json:"total"`
	Truncated      bool     `json:"truncated"`
	Items          []VMItem `json:"items"`
}

// VMOptions defines query arguments for QueryVM.
type VMOptions struct {
	Window         time.Duration
	IncludeSamples bool
	AllowExec      bool
}

// VMResult is the response payload for `kvtop vm`.
type VMResult struct {
	Schema        string  `json:"schema"`
	CollectedAt   string  `json:"collected_at"`
	WindowSeconds float64 `json:"window_seconds"`
	VM            VMItem  `json:"vm"`
}

// NodeItem describes a physical node's VM resource load and overcommit.
type NodeItem struct {
	NodeName                    string  `json:"node_name"`
	Ready                       bool    `json:"ready"`
	VMCount                     int     `json:"vm_count"`
	RunningVMCount              int     `json:"running_vm_count"`
	MigratingInCount            int     `json:"migrating_in_count"`
	MigratingOutCount           int     `json:"migrating_out_count"`
	CPUCoresUsed                float64 `json:"cpu_cores_used"`
	CPUCoresUsedSource          string  `json:"cpu_cores_used_source"`
	CPUAllocatableCores         int64   `json:"cpu_allocatable_cores"`
	CPUAllocatableCoresSource   string  `json:"cpu_allocatable_cores_source"`
	AllottedCPUs                int64   `json:"allotted_vcpus"`
	MemGuestUsedBytes           uint64  `json:"mem_guest_used_bytes"`
	MemGuestUsedBytesSource     string  `json:"mem_guest_used_bytes_source"`
	MemAllocatedBytes           uint64  `json:"mem_allocated_bytes"`
	NodeAllocatableBytes        int64   `json:"node_allocatable_bytes"`
	NodeAllocatableBytesSource  string  `json:"node_allocatable_bytes_source"`
	OvercommitRatio             float64 `json:"overcommit_ratio"`
	NetRxBytesPerSec            float64 `json:"net_rx_bytes_per_s"`
	NetRxBytesPerSecSource      string  `json:"net_rx_bytes_per_s_source"`
	NetTxBytesPerSec            float64 `json:"net_tx_bytes_per_s"`
	NetTxBytesPerSecSource      string  `json:"net_tx_bytes_per_s_source"`
	DiskIOPSTotal               float64 `json:"disk_iops_total"`
	DiskIOPSTotalSource         string  `json:"disk_iops_total_source"`
}

// NodesOptions defines query arguments for QueryNodes.
type NodesOptions struct {
	Window time.Duration
	Limit  int
}

// NodesResult is the response payload for `kvtop nodes`.
type NodesResult struct {
	Schema        string     `json:"schema"`
	CollectedAt   string     `json:"collected_at"`
	WindowSeconds float64    `json:"window_seconds"`
	Total         int        `json:"total"`
	Truncated     bool       `json:"truncated"`
	Items         []NodeItem `json:"items"`
}
