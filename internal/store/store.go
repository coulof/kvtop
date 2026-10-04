package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
)

const (
	// DefaultHistoryCapacity is the 300 samples per VM per metric ring buffer capacity.
	DefaultHistoryCapacity = 300
)

// VMMetrics tracks live state and time series history for a single VM.
type VMMetrics struct {
	Namespace    string
	Name         string
	Node         string
	Phase        string
	AllottedCPUs int64
	LastSeen     time.Time
	IP           string

	// Migration state from informers
	IsMigrating         bool
	MigrationSourceNode string
	MigrationTargetNode string
	MigrationPhase      string

	// Cache-aware counter rate trackers (handles virt-handler 5s cache hits)
	cpuTracker        FloatRateTracker
	netRxTracker      UintRateTracker
	netTxTracker      UintRateTracker
	readIOPSTracker   UintRateTracker
	writeIOPSTracker  UintRateTracker
	readBytesTracker  UintRateTracker
	writeBytesTracker UintRateTracker

	// Previous raw sample for calculating counter deltas
	prevSample collect.VMISample
	hasPrev    bool

	// Current computed rates & values
	CPUUsageCores        float64
	CPUSaturationPercent float64

	HasBalloonStats   bool
	MemoryUsedBytes   uint64
	MemoryTotalBytes  uint64
	MemoryUsedPercent float64

	NetRxBytesPerSec float64
	NetTxBytesPerSec float64

	StorageReadIOPS        float64
	StorageWriteIOPS       float64
	StorageTotalIOPS       float64
	StorageReadBytesPerSec float64
	StorageWriteBytesPerSec float64

	// History ring buffers (300 points)
	CPUHistory         *RingBuffer
	MemHistory         *RingBuffer
	NetRxHistory       *RingBuffer
	NetTxHistory       *RingBuffer
	StorageIOPSHistory *RingBuffer

	// Sizing & Metadata Context
	CPUTopology           kube.CPUTopology
	DedicatedCPUPlacement bool
	MemoryGuest           string
	MemoryRequested       string
	MemoryLimit           string
	InstanceType          string
	Preference            string
	EvictionStrategy      string
	Conditions            []kube.VMICondition
	Volumes               []kube.VMIVolumeInfo
}

// VMSnapshot represents an immutable point-in-time view of a VM for UI rendering.
type VMSnapshot struct {
	Namespace            string
	Name                 string
	Node                 string
	Phase                string
	AllottedCPUs         int64
	LastSeen             time.Time
	IP                   string
	CPUUsageCores        float64
	CPUSaturationPercent float64

	// Migration state
	IsMigrating         bool
	MigrationSourceNode string
	MigrationTargetNode string
	MigrationPhase      string

	HasBalloonStats   bool
	MemoryUsedBytes   uint64
	MemoryTotalBytes  uint64
	MemoryUsedPercent float64

	NetRxBytesPerSec float64
	NetTxBytesPerSec float64
	StorageTotalIOPS float64

	// Sizing & Metadata Context
	CPUTopology           kube.CPUTopology
	DedicatedCPUPlacement bool
	MemoryGuest           string
	MemoryRequested       string
	MemoryLimit           string
	InstanceType          string
	Preference            string
	EvictionStrategy      string
	Conditions            []kube.VMICondition
	Volumes               []kube.VMIVolumeInfo

	// History slices ordered oldest to newest (for sparklines & charts)
	CPUHistory         []float64
	MemHistory         []float64
	NetRxHistory       []float64
	NetTxHistory       []float64
	StorageIOPSHistory []float64
}

// Store maintains VM metrics, rates, ring buffers, and aggregations.
type Store struct {
	mu                  sync.RWMutex
	historyCapacity     int
	vms                 map[string]*VMMetrics
	nodeAllocatable     map[string]int64 // Node name -> allocatable memory bytes for overcommit
	nodeCPUs            map[string]int64
	nodeReady           map[string]bool
	namespaces          map[string]bool
	migrations          map[string]kube.MigrationInfo // namespace/vmiName -> MigrationInfo
	clusterCPUHistory   *RingBuffer
	clusterMemHistory   *RingBuffer
	clusterNetRxHistory *RingBuffer
	clusterNetTxHistory *RingBuffer
}

// NewStore creates a new metric store.
func NewStore(historyCapacity int) *Store {
	if historyCapacity <= 0 {
		historyCapacity = DefaultHistoryCapacity
	}
	return &Store{
		historyCapacity:     historyCapacity,
		vms:                 make(map[string]*VMMetrics),
		nodeAllocatable:     make(map[string]int64),
		nodeCPUs:            make(map[string]int64),
		nodeReady:           make(map[string]bool),
		namespaces:          make(map[string]bool),
		migrations:          make(map[string]kube.MigrationInfo),
		clusterCPUHistory:   NewRingBuffer(historyCapacity),
		clusterMemHistory:   NewRingBuffer(historyCapacity),
		clusterNetRxHistory: NewRingBuffer(historyCapacity),
		clusterNetTxHistory: NewRingBuffer(historyCapacity),
	}
}

// UpdateVMIs merges VMI metadata (allotted vCPUs, phase, node placement, sizing, context).
func (s *Store) UpdateVMIs(vmis []kube.VMIInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, vmi := range vmis {
		key := fmt.Sprintf("%s/%s", vmi.Namespace, vmi.Name)
		vm, exists := s.vms[key]
		if !exists {
			vm = s.newVMMetrics(vmi.Namespace, vmi.Name, vmi.NodeName)
			s.vms[key] = vm
		}
		s.applyVMIMetadata(vm, vmi)
	}
}

// SetNodeAllocatable sets the allocatable memory for a node (used for overcommit ratio).
func (s *Store) SetNodeAllocatable(nodeName string, memBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeAllocatable[nodeName] = memBytes
}

// PushSamples ingests a batch of raw collector samples, computes rates, and updates history.
func (s *Store) PushSamples(samples []collect.VMISample) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sample := range samples {
		key := fmt.Sprintf("%s/%s", sample.Namespace, sample.Name)
		vm, exists := s.vms[key]
		if !exists {
			vm = s.newVMMetrics(sample.Namespace, sample.Name, sample.Node)
			s.vms[key] = vm
		}

		// Detect node migration: if the VM changed nodes, reset trackers to prevent spikes
		migrated := vm.Node != "" && sample.Node != "" && vm.Node != sample.Node
		if sample.Node != "" {
			vm.Node = sample.Node
		}
		vm.LastSeen = sample.Timestamp

		if migrated {
			vm.cpuTracker.Reset()
			vm.netRxTracker.Reset()
			vm.netTxTracker.Reset()
			vm.readIOPSTracker.Reset()
			vm.writeIOPSTracker.Reset()
			vm.readBytesTracker.Reset()
			vm.writeBytesTracker.Reset()
			vm.hasPrev = false
		}

		// virt-handler caches domain stats on a ~5-second cycle.
		// Hold sustained rates during cache hits, decaying to 0 if idle beyond 6s.
		const cacheWindowSec = 6.0

		// CPU rate
		cpuRate := vm.cpuTracker.Update(sample.CPUUsageSeconds, sample.Timestamp, cacheWindowSec)
		vm.CPUUsageCores = cpuRate
		if vm.AllottedCPUs > 0 {
			vm.CPUSaturationPercent = (cpuRate / float64(vm.AllottedCPUs)) * 100
		}
		if vm.hasPrev {
			vm.CPUHistory.Push(sample.Timestamp, cpuRate)
		}

		// Network rates
		rxRate := vm.netRxTracker.Update(sample.NetRxBytesTotal, sample.Timestamp, cacheWindowSec)
		vm.NetRxBytesPerSec = rxRate
		txRate := vm.netTxTracker.Update(sample.NetTxBytesTotal, sample.Timestamp, cacheWindowSec)
		vm.NetTxBytesPerSec = txRate

		if vm.hasPrev {
			vm.NetRxHistory.Push(sample.Timestamp, rxRate)
			vm.NetTxHistory.Push(sample.Timestamp, txRate)
		}

		// Storage IOPS
		rIOPS := vm.readIOPSTracker.Update(sample.StorageReadIOPSTotal, sample.Timestamp, cacheWindowSec)
		vm.StorageReadIOPS = rIOPS
		wIOPS := vm.writeIOPSTracker.Update(sample.StorageWriteIOPSTotal, sample.Timestamp, cacheWindowSec)
		vm.StorageWriteIOPS = wIOPS
		vm.StorageTotalIOPS = rIOPS + wIOPS
		if vm.hasPrev {
			vm.StorageIOPSHistory.Push(sample.Timestamp, vm.StorageTotalIOPS)
		}

		// Storage Traffic Bytes/s
		vm.StorageReadBytesPerSec = vm.readBytesTracker.Update(sample.StorageReadBytesTotal, sample.Timestamp, cacheWindowSec)
		vm.StorageWriteBytesPerSec = vm.writeBytesTracker.Update(sample.StorageWriteBytesTotal, sample.Timestamp, cacheWindowSec)

		// Guest memory calculation (derived from balloon stats)
		vm.HasBalloonStats = sample.HasBalloonStats
		if sample.HasBalloonStats && sample.MemoryDomainBytes > 0 {
			vm.MemoryTotalBytes = sample.MemoryDomainBytes
			if sample.MemoryUsableBytes <= sample.MemoryDomainBytes {
				vm.MemoryUsedBytes = sample.MemoryDomainBytes - sample.MemoryUsableBytes
			} else {
				vm.MemoryUsedBytes = 0
			}
			vm.MemoryUsedPercent = (float64(vm.MemoryUsedBytes) / float64(vm.MemoryTotalBytes)) * 100
			vm.MemHistory.Push(sample.Timestamp, float64(vm.MemoryUsedBytes))
		}

		vm.prevSample = sample
		vm.hasPrev = true
	}

	// Update cluster-wide time series history
	var (
		totalCPU float64
		totalMem uint64
		totalRx  float64
		totalTx  float64
		latestT  time.Time
	)
	for _, vm := range s.vms {
		totalCPU += vm.CPUUsageCores
		totalMem += vm.MemoryUsedBytes
		totalRx += vm.NetRxBytesPerSec
		totalTx += vm.NetTxBytesPerSec
		if vm.LastSeen.After(latestT) {
			latestT = vm.LastSeen
		}
	}
	if !latestT.IsZero() {
		s.clusterCPUHistory.Push(latestT, totalCPU)
		s.clusterMemHistory.Push(latestT, float64(totalMem))
		s.clusterNetRxHistory.Push(latestT, totalRx)
		s.clusterNetTxHistory.Push(latestT, totalTx)
	}
}

// GetVM returns a snapshot of a specific VM.
func (s *Store) GetVM(namespace, name string) (VMSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	vm, exists := s.vms[key]
	if !exists {
		return VMSnapshot{}, false
	}
	return s.snapshotVM(vm), true
}

// GetAllVMs returns snapshots of all tracked VMs.
func (s *Store) GetAllVMs() []VMSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]VMSnapshot, 0, len(s.vms))
	for _, vm := range s.vms {
		res = append(res, s.snapshotVM(vm))
	}
	return res
}

// VMHistoryPoints holds the raw timestamped points for a VM's metrics.
type VMHistoryPoints struct {
	CPUHistory         []Point
	MemHistory         []Point
	NetRxHistory       []Point
	NetTxHistory       []Point
	StorageIOPSHistory []Point
}

// GetVMHistoryPoints returns timestamped points for a specific VM.
func (s *Store) GetVMHistoryPoints(namespace, name string) (VMHistoryPoints, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	vm, exists := s.vms[key]
	if !exists {
		return VMHistoryPoints{}, false
	}
	return VMHistoryPoints{
		CPUHistory:         vm.CPUHistory.Points(),
		MemHistory:         vm.MemHistory.Points(),
		NetRxHistory:       vm.NetRxHistory.Points(),
		NetTxHistory:       vm.NetTxHistory.Points(),
		StorageIOPSHistory: vm.StorageIOPSHistory.Points(),
	}, true
}

// GetAllVMHistoryPoints returns timestamped points for all VMs keyed by namespace/name.
func (s *Store) GetAllVMHistoryPoints() map[string]VMHistoryPoints {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make(map[string]VMHistoryPoints, len(s.vms))
	for k, vm := range s.vms {
		res[k] = VMHistoryPoints{
			CPUHistory:         vm.CPUHistory.Points(),
			MemHistory:         vm.MemHistory.Points(),
			NetRxHistory:       vm.NetRxHistory.Points(),
			NetTxHistory:       vm.NetTxHistory.Points(),
			StorageIOPSHistory: vm.StorageIOPSHistory.Points(),
		}
	}
	return res
}

func (s *Store) newVMMetrics(ns, name, node string) *VMMetrics {
	return &VMMetrics{
		Namespace:          ns,
		Name:               name,
		Node:               node,
		AllottedCPUs:       1,
		CPUHistory:         NewRingBuffer(s.historyCapacity),
		MemHistory:         NewRingBuffer(s.historyCapacity),
		NetRxHistory:       NewRingBuffer(s.historyCapacity),
		NetTxHistory:       NewRingBuffer(s.historyCapacity),
		StorageIOPSHistory: NewRingBuffer(s.historyCapacity),
	}
}

func (s *Store) snapshotVM(vm *VMMetrics) VMSnapshot {
	key := fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
	isMigrating := vm.IsMigrating
	srcNode := vm.MigrationSourceNode
	tgtNode := vm.MigrationTargetNode
	migPhase := vm.MigrationPhase

	if mig, ok := s.migrations[key]; ok && mig.Active {
		isMigrating = true
		srcNode = mig.SourceNode
		tgtNode = mig.TargetNode
		migPhase = mig.Phase
	}

	return VMSnapshot{
		Namespace:            vm.Namespace,
		Name:                 vm.Name,
		Node:                 vm.Node,
		Phase:                vm.Phase,
		AllottedCPUs:         vm.AllottedCPUs,
		LastSeen:             vm.LastSeen,
		IP:                   vm.IP,
		CPUUsageCores:        vm.CPUUsageCores,
		CPUSaturationPercent: vm.CPUSaturationPercent,
		IsMigrating:          isMigrating,
		MigrationSourceNode:  srcNode,
		MigrationTargetNode:  tgtNode,
		MigrationPhase:       migPhase,
		HasBalloonStats:      vm.HasBalloonStats,
		MemoryUsedBytes:      vm.MemoryUsedBytes,
		MemoryTotalBytes:     vm.MemoryTotalBytes,
		MemoryUsedPercent:    vm.MemoryUsedPercent,
		NetRxBytesPerSec:     vm.NetRxBytesPerSec,
		NetTxBytesPerSec:     vm.NetTxBytesPerSec,
		StorageTotalIOPS:     vm.StorageTotalIOPS,
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
		CPUHistory:           vm.CPUHistory.Values(),
		MemHistory:           vm.MemHistory.Values(),
		NetRxHistory:         vm.NetRxHistory.Values(),
		NetTxHistory:         vm.NetTxHistory.Values(),
		StorageIOPSHistory:   vm.StorageIOPSHistory.Values(),
	}
}

func (s *Store) applyVMIMetadata(vm *VMMetrics, vmi kube.VMIInfo) {
	if vmi.NodeName != "" {
		if vm.Node != "" && vm.Node != vmi.NodeName {
			// Node move detected: clear migration and reset trackers to prevent spikes
			key := fmt.Sprintf("%s/%s", vm.Namespace, vm.Name)
			delete(s.migrations, key)
			vm.IsMigrating = false
			vm.hasPrev = false
			vm.cpuTracker.Reset()
			vm.netRxTracker.Reset()
			vm.netTxTracker.Reset()
			vm.readIOPSTracker.Reset()
			vm.writeIOPSTracker.Reset()
			vm.readBytesTracker.Reset()
			vm.writeBytesTracker.Reset()
		}
		vm.Node = vmi.NodeName
	}
	if vmi.Phase != "" {
		vm.Phase = vmi.Phase
	}
	if vmi.CPUCores > 0 {
		vm.AllottedCPUs = vmi.CPUCores
	}
	if vmi.IP != "" {
		vm.IP = vmi.IP
	}
	vm.CPUTopology = vmi.CPUTopology
	vm.DedicatedCPUPlacement = vmi.DedicatedCPUPlacement
	if vmi.MemoryGuest != "" {
		vm.MemoryGuest = vmi.MemoryGuest
	}
	if vmi.MemoryRequested != "" {
		vm.MemoryRequested = vmi.MemoryRequested
	}
	if vmi.MemoryLimit != "" {
		vm.MemoryLimit = vmi.MemoryLimit
	}
	if vmi.InstanceType != "" {
		vm.InstanceType = vmi.InstanceType
	}
	if vmi.Preference != "" {
		vm.Preference = vmi.Preference
	}
	if vmi.EvictionStrategy != "" {
		vm.EvictionStrategy = vmi.EvictionStrategy
	}
	if len(vmi.Conditions) > 0 {
		vm.Conditions = vmi.Conditions
	}
	if len(vmi.Volumes) > 0 {
		vm.Volumes = vmi.Volumes
	}
	s.namespaces[vmi.Namespace] = true
}

// OnVMIUpdated is called by the VMI informer when a VMI is created or modified.
func (s *Store) OnVMIUpdated(vmi kube.VMIInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s/%s", vmi.Namespace, vmi.Name)
	vm, exists := s.vms[key]
	if !exists {
		vm = s.newVMMetrics(vmi.Namespace, vmi.Name, vmi.NodeName)
		s.vms[key] = vm
	}
	s.applyVMIMetadata(vm, vmi)
}

// OnVMIDeleted is called by the VMI informer when a VMI is deleted.
func (s *Store) OnVMIDeleted(namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s/%s", namespace, name)
	delete(s.vms, key)
	delete(s.migrations, key)
}

// OnMigrationUpdated is called when a VirtualMachineInstanceMigration is created or updated.
func (s *Store) OnMigrationUpdated(mig kube.MigrationInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%s/%s", mig.Namespace, mig.VMIName)
	if mig.Active {
		s.migrations[key] = mig
		if vm, exists := s.vms[key]; exists {
			vm.IsMigrating = true
			vm.MigrationSourceNode = mig.SourceNode
			vm.MigrationTargetNode = mig.TargetNode
			vm.MigrationPhase = mig.Phase
		}
	} else {
		delete(s.migrations, key)
		if vm, exists := s.vms[key]; exists {
			vm.IsMigrating = false
			vm.MigrationSourceNode = ""
			vm.MigrationTargetNode = ""
			vm.MigrationPhase = ""
		}
	}
}

// OnMigrationDeleted is called when a VirtualMachineInstanceMigration is deleted.
func (s *Store) OnMigrationDeleted(namespace, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, m := range s.migrations {
		if m.Namespace == namespace && (m.MigrationName == name || m.VMIName == name) {
			delete(s.migrations, k)
			if vm, exists := s.vms[k]; exists {
				vm.IsMigrating = false
				vm.MigrationSourceNode = ""
				vm.MigrationTargetNode = ""
				vm.MigrationPhase = ""
			}
		}
	}
}

// OnNodeUpdated is called by the Node informer when a node is added or updated.
func (s *Store) OnNodeUpdated(node kube.NodeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodeAllocatable[node.Name] = node.AllocatableMem
	s.nodeCPUs[node.Name] = node.AllocatableCPUs
	s.nodeReady[node.Name] = node.Ready
}

// OnNodeDeleted is called by the Node informer when a node is deleted.
func (s *Store) OnNodeDeleted(nodeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.nodeAllocatable, nodeName)
	delete(s.nodeCPUs, nodeName)
	delete(s.nodeReady, nodeName)
}

// OnNamespaceUpdated records active namespaces.
func (s *Store) OnNamespaceUpdated(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.namespaces[namespace] = true
}

// OnNamespaceDeleted removes deleted namespaces.
func (s *Store) OnNamespaceDeleted(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.namespaces, namespace)
}
