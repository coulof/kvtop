package store

import (
	"sort"
	"time"
)

// NodeAggregate contains aggregated VM metric sums and capacity for a single cluster node.
type NodeAggregate struct {
	NodeName             string
	Ready                bool
	VMCount              int
	RunningVMCount       int
	MigratingInCount     int
	MigratingOutCount    int
	CPUUsageCores        float64
	AllottedCPUs         int64
	MemoryUsedBytes      uint64
	MemoryAllocatedBytes uint64
	NodeAllocatableBytes int64
	NodeAllocatableCPUs  int64
	OvercommitRatio      float64
	NetRxBytesPerSec     float64
	NetTxBytesPerSec     float64
	StorageTotalIOPS     float64
}

// NamespaceAggregate contains aggregated VM metric sums for a Kubernetes namespace.
type NamespaceAggregate struct {
	Namespace        string
	VMCount          int
	CPUUsageCores    float64
	AllottedCPUs     int64
	MemoryUsedBytes  uint64
	NetRxBytesPerSec float64
	NetTxBytesPerSec float64
	StorageTotalIOPS float64
}

// ClusterTotals summarizes cluster-wide VM resource usage.
type ClusterTotals struct {
	TotalVMs             int
	RunningVMs           int
	MigratingVMs         int
	NodesReady           int
	NodesTotal           int
	CPUUsageCores        float64
	AllottedCPUs         int64
	MemoryUsedBytes      uint64
	MemoryAllocatedBytes uint64
	NodeAllocatableBytes int64
	OvercommitRatio      float64
	NetRxBytesPerSec     float64
	NetTxBytesPerSec     float64
	StorageTotalIOPS     float64

	// History slices for braille charts
	CPUHistory   []float64
	MemHistory   []float64
	NetRxHistory []float64
	NetTxHistory []float64
}

// StoreSnapshot provides a complete, consistent snapshot of VMs and aggregates for the UI.
type StoreSnapshot struct {
	Timestamp           time.Time
	VMs                 []VMSnapshot
	Nodes               []NodeAggregate
	Namespaces          []NamespaceAggregate
	ClusterTotals       ClusterTotals
	ActiveNamespace     string // Filter applied, if any
	KnownNamespaces     []string
}

// Snapshot generates an immutable point-in-time snapshot with node and namespace aggregations.
// If namespaceFilter is non-empty, VMs and ClusterTotals are scoped to that namespace,
// while NodeAggregates remain cluster-wide as per design specification.
func (s *Store) Snapshot(namespaceFilter string) StoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var (
		vmsList   []VMSnapshot
		nodeMap   = make(map[string]*NodeAggregate)
		nsMap     = make(map[string]*NamespaceAggregate)
		cluster   ClusterTotals
	)

	// Ensure all known nodes have an entry in nodeMap even if 0 VMs are placed on them
	for node, allocMem := range s.nodeAllocatable {
		ready := true
		if r, ok := s.nodeReady[node]; ok {
			ready = r
		}
		allocCPU := s.nodeCPUs[node]
		nodeMap[node] = &NodeAggregate{
			NodeName:             node,
			NodeAllocatableBytes: allocMem,
			NodeAllocatableCPUs:  allocCPU,
			Ready:                ready,
		}
	}

	for _, vm := range s.vms {
		snap := s.snapshotVM(vm)

		// Check active migration
		if snap.IsMigrating {
			cluster.MigratingVMs++
			if snap.MigrationSourceNode != "" {
				if sna, ok := nodeMap[snap.MigrationSourceNode]; ok {
					sna.MigratingOutCount++
				}
			}
			if snap.MigrationTargetNode != "" {
				if tna, ok := nodeMap[snap.MigrationTargetNode]; ok {
					tna.MigratingInCount++
				}
			}
		}

		// 1. Update Node Aggregates (Always cluster-wide)
		if vm.Node != "" {
			na, exists := nodeMap[vm.Node]
			if !exists {
				allocMem := s.nodeAllocatable[vm.Node]
				allocCPU := s.nodeCPUs[vm.Node]
				ready := true
				if r, ok := s.nodeReady[vm.Node]; ok {
					ready = r
				}
				na = &NodeAggregate{
					NodeName:             vm.Node,
					NodeAllocatableBytes: allocMem,
					NodeAllocatableCPUs:  allocCPU,
					Ready:                ready,
				}
				nodeMap[vm.Node] = na
			}
			na.VMCount++
			if vm.Phase == "Running" || vm.Phase == "" {
				na.RunningVMCount++
			}
			na.CPUUsageCores += vm.CPUUsageCores
			na.AllottedCPUs += vm.AllottedCPUs
			na.MemoryUsedBytes += vm.MemoryUsedBytes
			na.MemoryAllocatedBytes += vm.MemoryTotalBytes
			na.NetRxBytesPerSec += vm.NetRxBytesPerSec
			na.NetTxBytesPerSec += vm.NetTxBytesPerSec
			na.StorageTotalIOPS += vm.StorageTotalIOPS
		}

		// 2. Update Namespace Aggregates (All namespaces)
		nsa, exists := nsMap[vm.Namespace]
		if !exists {
			nsa = &NamespaceAggregate{
				Namespace: vm.Namespace,
			}
			nsMap[vm.Namespace] = nsa
		}
		nsa.VMCount++
		nsa.CPUUsageCores += vm.CPUUsageCores
		nsa.AllottedCPUs += vm.AllottedCPUs
		nsa.MemoryUsedBytes += vm.MemoryUsedBytes
		nsa.NetRxBytesPerSec += vm.NetRxBytesPerSec
		nsa.NetTxBytesPerSec += vm.NetTxBytesPerSec
		nsa.StorageTotalIOPS += vm.StorageTotalIOPS

		// 3. Filter check for table and cluster totals
		if namespaceFilter != "" && vm.Namespace != namespaceFilter {
			continue
		}

		vmsList = append(vmsList, snap)

		// 4. Update Cluster / Active Scope Totals
		cluster.TotalVMs++
		if vm.Phase == "Running" || vm.Phase == "" {
			cluster.RunningVMs++
		}
		cluster.CPUUsageCores += vm.CPUUsageCores
		cluster.AllottedCPUs += vm.AllottedCPUs
		cluster.MemoryUsedBytes += vm.MemoryUsedBytes
		cluster.MemoryAllocatedBytes += vm.MemoryTotalBytes
		cluster.NetRxBytesPerSec += vm.NetRxBytesPerSec
		cluster.NetTxBytesPerSec += vm.NetTxBytesPerSec
		cluster.StorageTotalIOPS += vm.StorageTotalIOPS
	}

	// Compute overcommit ratios
	var totalAllocatable int64
	for _, na := range nodeMap {
		if na.NodeAllocatableBytes > 0 {
			na.OvercommitRatio = float64(na.MemoryAllocatedBytes) / float64(na.NodeAllocatableBytes)
			totalAllocatable += na.NodeAllocatableBytes
		}
	}
	cluster.NodeAllocatableBytes = totalAllocatable
	if totalAllocatable > 0 {
		cluster.OvercommitRatio = float64(cluster.MemoryAllocatedBytes) / float64(totalAllocatable)
	}

	// Attach cluster-wide time series history
	cluster.CPUHistory = s.clusterCPUHistory.Values()
	cluster.MemHistory = s.clusterMemHistory.Values()
	cluster.NetRxHistory = s.clusterNetRxHistory.Values()
	cluster.NetTxHistory = s.clusterNetTxHistory.Values()

	// Sort nodes alphabetically
	nodesList := make([]NodeAggregate, 0, len(nodeMap))
	for _, na := range nodeMap {
		nodesList = append(nodesList, *na)
	}
	sort.Slice(nodesList, func(i, j int) bool {
		return nodesList[i].NodeName < nodesList[j].NodeName
	})

	// Sort namespaces alphabetically
	nsList := make([]NamespaceAggregate, 0, len(nsMap))
	for _, nsa := range nsMap {
		nsList = append(nsList, *nsa)
	}
	sort.Slice(nsList, func(i, j int) bool {
		return nsList[i].Namespace < nsList[j].Namespace
	})

	cluster.NodesTotal = len(nodeMap)
	for _, na := range nodeMap {
		if na.Ready {
			cluster.NodesReady++
		}
	}

	knownNS := make([]string, 0, len(s.namespaces))
	for ns := range s.namespaces {
		knownNS = append(knownNS, ns)
	}
	sort.Strings(knownNS)

	return StoreSnapshot{
		Timestamp:       now,
		VMs:             vmsList,
		Nodes:           nodesList,
		Namespaces:      nsList,
		ClusterTotals:   cluster,
		ActiveNamespace: namespaceFilter,
		KnownNamespaces: knownNS,
	}
}
