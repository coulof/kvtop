package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/store"
)

// SnapshotProvider defines the interface required by the TUI to read cluster snapshots.
type SnapshotProvider interface {
	Snapshot(namespaceFilter string) store.StoreSnapshot
	StreamVirshStats(ctx context.Context, namespace, vmiName string, statsChan chan<- collect.VirshStats) (string, error)
}

// Engine implements the shared read and query API over the store and Kubernetes informers.
type Engine struct {
	store      *store.Store
	kClient    *kube.Client
	replayDir  string
	mu         sync.RWMutex
	clusterErr error
}

// NewEngine creates a new query engine.
func NewEngine(st *store.Store, kClient *kube.Client, replayDir string) *Engine {
	return &Engine{
		store:     st,
		kClient:   kClient,
		replayDir: replayDir,
	}
}

// SetClusterError records an asynchronous cluster connection error.
func (e *Engine) SetClusterError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clusterErr = err
}

// ClusterError returns the latest cluster connection error if any.
func (e *Engine) ClusterError() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.clusterErr
}

// Snapshot delegates to store.Snapshot for real-time TUI views.
func (e *Engine) Snapshot(namespaceFilter string) store.StoreSnapshot {
	return e.store.Snapshot(namespaceFilter)
}

// StreamVirshStats starts streaming virsh domstats (live exec or replay).
func (e *Engine) StreamVirshStats(ctx context.Context, namespace, vmiName string, statsChan chan<- collect.VirshStats) (string, error) {
	if e.replayDir != "" {
		podName := fmt.Sprintf("virt-launcher-%s-replay", vmiName)
		baseStats, err := collect.LoadReplayVirshStats(e.replayDir)
		if err != nil {
			return "", err
		}
		go func() {
			statsChan <- baseStats
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					current := baseStats
					current.Timestamp = time.Now()
					statsChan <- current
				}
			}
		}()
		return podName, nil
	}

	if e.kClient != nil {
		podName, err := e.kClient.FindLauncherPod(ctx, namespace, vmiName)
		if err != nil {
			return "", err
		}
		stream, err := e.kClient.StreamVirshDomstatsExec(ctx, namespace, podName)
		if err != nil {
			return podName, err
		}
		go func() {
			defer stream.Close()
			collect.StreamVirshDomstats(stream, statsChan, ctx.Done())
		}()
		return podName, nil
	}

	return "", fmt.Errorf("neither kubernetes client nor replay directory is configured")
}

// QueryTop returns the top VMs sorted and filtered per TopOptions.
func (e *Engine) QueryTop(ctx context.Context, opts TopOptions) (*TopResult, error) {
	if opts.Window <= 0 {
		opts.Window = 15 * time.Second
	}
	if opts.Window < 10*time.Second {
		return nil, ErrWindowTooSmall
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.SortBy == "" {
		opts.SortBy = "cpu"
	}

	now := time.Now()
	cutoff := now.Add(-opts.Window)

	vms := e.store.GetAllVMs()
	if len(vms) == 0 {
		if cErr := e.ClusterError(); cErr != nil {
			return nil, fmt.Errorf("cluster connection error: %w", cErr)
		}
	}
	allPoints := e.store.GetAllVMHistoryPoints()

	nsFilterMap := make(map[string]bool)
	for _, ns := range opts.Namespaces {
		trimmed := strings.TrimSpace(ns)
		if trimmed != "" {
			nsFilterMap[trimmed] = true
		}
	}

	var items []VMItem
	for _, vm := range vms {
		// Namespace filter
		if len(nsFilterMap) > 0 && !nsFilterMap[vm.Namespace] {
			continue
		}
		// Node filter
		if opts.Node != "" && vm.Node != opts.Node {
			continue
		}

		key := fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
		pts := allPoints[key]

		item := e.buildVMItem(vm, pts, cutoff, now, opts.IncludeSamples)
		items = append(items, item)
	}

	total := len(items)

	// Sort items
	e.sortVMItems(items, opts.SortBy, opts.BySaturation)

	truncated := false
	if len(items) > opts.Limit {
		truncated = true
		items = items[:opts.Limit]
	}

	return &TopResult{
		Schema:        SchemaVersion,
		CollectedAt:   now.UTC().Format(time.RFC3339),
		WindowSeconds: opts.Window.Seconds(),
		Total:         total,
		Truncated:     truncated,
		Items:         items,
	}, nil
}

// QueryVM retrieves detailed metric summaries and context for a single VM.
func (e *Engine) QueryVM(ctx context.Context, namespace, name string, opts VMOptions) (*VMResult, error) {
	if opts.Window <= 0 {
		opts.Window = 15 * time.Second
	}
	if opts.Window < 10*time.Second {
		return nil, ErrWindowTooSmall
	}

	vm, exists := e.store.GetVM(namespace, name)
	if !exists {
		if cErr := e.ClusterError(); cErr != nil {
			return nil, fmt.Errorf("cluster connection error: %w", cErr)
		}
		return nil, fmt.Errorf("%w: %s/%s", ErrVMNotFound, namespace, name)
	}

	pts, _ := e.store.GetVMHistoryPoints(namespace, name)
	now := time.Now()
	cutoff := now.Add(-opts.Window)

	item := e.buildVMItem(vm, pts, cutoff, now, opts.IncludeSamples)

	return &VMResult{
		Schema:        SchemaVersion,
		CollectedAt:   now.UTC().Format(time.RFC3339),
		WindowSeconds: opts.Window.Seconds(),
		VM:            item,
	}, nil
}

// QueryNodes returns aggregated VM resource usage per node.
func (e *Engine) QueryNodes(ctx context.Context, opts NodesOptions) (*NodesResult, error) {
	if opts.Window <= 0 {
		opts.Window = 15 * time.Second
	}

	now := time.Now()
	snap := e.store.Snapshot("")
	if len(snap.Nodes) == 0 {
		if cErr := e.ClusterError(); cErr != nil {
			return nil, fmt.Errorf("cluster connection error: %w", cErr)
		}
	}

	var items []NodeItem
	for _, n := range snap.Nodes {
		item := NodeItem{
			NodeName:                   n.NodeName,
			Ready:                      n.Ready,
			VMCount:                    n.VMCount,
			RunningVMCount:             n.RunningVMCount,
			MigratingInCount:           n.MigratingInCount,
			MigratingOutCount:          n.MigratingOutCount,
			CPUCoresUsed:               n.CPUUsageCores,
			CPUCoresUsedSource:         SourceVirtHandler,
			CPUAllocatableCores:        n.NodeAllocatableCPUs,
			CPUAllocatableCoresSource:  SourceKubernetes,
			AllottedCPUs:               n.AllottedCPUs,
			MemGuestUsedBytes:          n.MemoryUsedBytes,
			MemGuestUsedBytesSource:    SourceVirtHandler,
			MemAllocatedBytes:          n.MemoryAllocatedBytes,
			NodeAllocatableBytes:       n.NodeAllocatableBytes,
			NodeAllocatableBytesSource: SourceKubernetes,
			OvercommitRatio:            n.OvercommitRatio,
			NetRxBytesPerSec:           n.NetRxBytesPerSec,
			NetRxBytesPerSecSource:     SourceVirtHandler,
			NetTxBytesPerSec:           n.NetTxBytesPerSec,
			NetTxBytesPerSecSource:     SourceVirtHandler,
			DiskIOPSTotal:              n.StorageTotalIOPS,
			DiskIOPSTotalSource:        SourceVirtHandler,
		}
		items = append(items, item)
	}

	total := len(items)
	truncated := false
	if opts.Limit > 0 && len(items) > opts.Limit {
		truncated = true
		items = items[:opts.Limit]
	}

	return &NodesResult{
		Schema:        SchemaVersion,
		CollectedAt:   now.UTC().Format(time.RFC3339),
		WindowSeconds: opts.Window.Seconds(),
		Total:         total,
		Truncated:     truncated,
		Items:         items,
	}, nil
}

func (e *Engine) buildVMItem(vm store.VMSnapshot, pts store.VMHistoryPoints, cutoff, now time.Time, includeSamples bool) VMItem {
	cpuSummary := ComputeSummary(pts.CPUHistory, cutoff)
	cpuSatSummary := ComputeSaturationSummary(cpuSummary, vm.AllottedCPUs)
	rxSummary := ComputeSummary(pts.NetRxHistory, cutoff)
	txSummary := ComputeSummary(pts.NetTxHistory, cutoff)
	iopsSummary := ComputeSummary(pts.StorageIOPSHistory, cutoff)

	var (
		memSummary       *MetricSummary
		memReason        string
		memSource        string
		memTotal         *uint64
		memPctSummary    *MetricSummary
		diskLatency      *MetricSummary
		diskLatencyReason string
		diskLatencySource string
		ipStr            *string
		ipReason         string
		migInfo          *MigrationInfo
	)

	if vm.HasBalloonStats && vm.MemoryTotalBytes > 0 {
		memSummary = ComputeSummary(pts.MemHistory, cutoff)
		memSource = SourceVirtHandler
		tot := vm.MemoryTotalBytes
		memTotal = &tot
		memPctSummary = ComputePercentSummary(memSummary, tot)
	} else {
		memReason = "no balloon stats"
	}

	// Disk latency: virt-handler reports cumulative I/O seconds per disk;
	// if ops or time delta is zero/unavailable in window, report reason.
	diskLatencyReason = "no io in window"
	diskLatencySource = SourceVirtHandler

	if vm.IP != "" {
		s := vm.IP
		ipStr = &s
	} else {
		ipReason = "no ip reported"
	}

	if vm.IsMigrating {
		migInfo = &MigrationInfo{
			SourceNode: vm.MigrationSourceNode,
			TargetNode: vm.MigrationTargetNode,
			Phase:      vm.MigrationPhase,
		}
	}

	stale := false
	staleAge := 0.0
	if !vm.LastSeen.IsZero() {
		age := now.Sub(vm.LastSeen).Seconds()
		if age > 10.0 {
			stale = true
			staleAge = age
		}
	}

	item := VMItem{
		Namespace:               vm.Namespace,
		Name:                    vm.Name,
		Node:                    vm.Node,
		Phase:                   vm.Phase,
		IP:                      ipStr,
		IPReason:                ipReason,
		AllottedVCPUs:           vm.AllottedCPUs,
		CPUCoresUsed:            cpuSummary,
		CPUCoresUsedSource:      SourceVirtHandler,
		CPUSaturationPercent:    cpuSatSummary,
		MemGuestUsedBytes:       memSummary,
		MemGuestUsedBytesReason: memReason,
		MemGuestUsedBytesSource: memSource,
		MemGuestTotalBytes:      memTotal,
		MemGuestUsedPercent:     memPctSummary,
		NetRxBytesPerSec:        rxSummary,
		NetRxBytesPerSecSource:  SourceVirtHandler,
		NetTxBytesPerSec:        txSummary,
		NetTxBytesPerSecSource:  SourceVirtHandler,
		DiskIOPSTotal:           iopsSummary,
		DiskIOPSTotalSource:     SourceVirtHandler,
		DiskLatencyMs:           diskLatency,
		DiskLatencyMsReason:     diskLatencyReason,
		DiskLatencyMsSource:     diskLatencySource,
		IsMigrating:             vm.IsMigrating,
		Migration:               migInfo,
		Stale:                   stale,
		StaleAgeSeconds:         staleAge,
		Context: &VMContext{
			CPUTopology:           vm.CPUTopology,
			DedicatedCPUPlacement: vm.DedicatedCPUPlacement,
			MemoryGuest:           vm.MemoryGuest,
			MemoryRequested:       vm.MemoryRequested,
			MemoryLimit:           vm.MemoryLimit,
			InstanceType:          vm.InstanceType,
			Preference:            vm.Preference,
			EvictionStrategy:      vm.EvictionStrategy,
			Conditions:            vm.Conditions,
			Volumes:               vm.Volumes,
		},
	}

	if includeSamples {
		item.Samples = &VMMetricSamples{
			CPUUsageCores:        ConvertToTimestampedPoints(pts.CPUHistory, cutoff),
			MemoryGuestUsedBytes: ConvertToTimestampedPoints(pts.MemHistory, cutoff),
			NetRxBytesPerSec:     ConvertToTimestampedPoints(pts.NetRxHistory, cutoff),
			NetTxBytesPerSec:     ConvertToTimestampedPoints(pts.NetTxHistory, cutoff),
			StorageTotalIOPS:     ConvertToTimestampedPoints(pts.StorageIOPSHistory, cutoff),
		}
	}

	return item
}

func (e *Engine) sortVMItems(items []VMItem, sortBy string, bySaturation bool) {
	tieBreaker := func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	}

	sort.Slice(items, func(i, j int) bool {
		switch sortBy {
		case "cpu":
			if bySaturation {
				satI := 0.0
				if items[i].CPUSaturationPercent != nil {
					satI = items[i].CPUSaturationPercent.Last
				}
				satJ := 0.0
				if items[j].CPUSaturationPercent != nil {
					satJ = items[j].CPUSaturationPercent.Last
				}
				if satI != satJ {
					return satI > satJ
				}
				return tieBreaker(i, j)
			}
			cpuI := 0.0
			if items[i].CPUCoresUsed != nil {
				cpuI = items[i].CPUCoresUsed.Last
			}
			cpuJ := 0.0
			if items[j].CPUCoresUsed != nil {
				cpuJ = items[j].CPUCoresUsed.Last
			}
			if cpuI != cpuJ {
				return cpuI > cpuJ
			}
			return tieBreaker(i, j)

		case "mem":
			// Items with balloon stats take precedence over those without
			hasMemI := items[i].MemGuestUsedBytes != nil
			hasMemJ := items[j].MemGuestUsedBytes != nil
			if hasMemI != hasMemJ {
				return hasMemI
			}
			memI := 0.0
			if hasMemI {
				memI = items[i].MemGuestUsedBytes.Last
			}
			memJ := 0.0
			if hasMemJ {
				memJ = items[j].MemGuestUsedBytes.Last
			}
			if memI != memJ {
				return memI > memJ
			}
			return tieBreaker(i, j)

		case "net":
			rxI, txI := 0.0, 0.0
			if items[i].NetRxBytesPerSec != nil {
				rxI = items[i].NetRxBytesPerSec.Last
			}
			if items[i].NetTxBytesPerSec != nil {
				txI = items[i].NetTxBytesPerSec.Last
			}
			rxJ, txJ := 0.0, 0.0
			if items[j].NetRxBytesPerSec != nil {
				rxJ = items[j].NetRxBytesPerSec.Last
			}
			if items[j].NetTxBytesPerSec != nil {
				txJ = items[j].NetTxBytesPerSec.Last
			}
			totI := rxI + txI
			totJ := rxJ + txJ
			if totI != totJ {
				return totI > totJ
			}
			return tieBreaker(i, j)

		case "disk":
			ioI := 0.0
			if items[i].DiskIOPSTotal != nil {
				ioI = items[i].DiskIOPSTotal.Last
			}
			ioJ := 0.0
			if items[j].DiskIOPSTotal != nil {
				ioJ = items[j].DiskIOPSTotal.Last
			}
			if ioI != ioJ {
				return ioI > ioJ
			}
			return tieBreaker(i, j)

		default:
			return tieBreaker(i, j)
		}
	})
}
