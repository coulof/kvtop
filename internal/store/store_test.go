package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/store"
)

func TestStore_PushAndRates(t *testing.T) {
	st := store.NewStore(300)

	// Set VMI metadata: 2 cores
	st.UpdateVMIs([]kube.VMIInfo{
		{
			Namespace: "default",
			Name:      "vm-1",
			NodeName:  "hv-01",
			Phase:     "Running",
			CPUCores:  2,
		},
	})

	t0 := time.Now()

	// Initial scrape tick
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "vm-1",
			Node:              "hv-01",
			Timestamp:         t0,
			CPUUsageSeconds:   100.0,
			HasBalloonStats:   true,
			MemoryDomainBytes: 4 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 3 * 1024 * 1024 * 1024, // 1GB used
			NetRxBytesTotal:   10000,
			NetTxBytesTotal:   5000,
		},
	})

	// Initial tick should have 0 rates since there's no delta
	vm, ok := st.GetVM("default", "vm-1")
	if !ok {
		t.Fatalf("expected vm-1 to exist in store")
	}
	if vm.CPUUsageCores != 0 {
		t.Errorf("expected 0 CPU rate on initial tick, got %f", vm.CPUUsageCores)
	}
	if !vm.HasBalloonStats || vm.MemoryUsedBytes != 1024*1024*1024 {
		t.Errorf("expected 1GB memory used, got %d", vm.MemoryUsedBytes)
	}

	// Second tick 2 seconds later: CPU advanced by 2.0s (1.0 core used), NetRx +2000B (1000B/s)
	t1 := t0.Add(2 * time.Second)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "vm-1",
			Node:              "hv-01",
			Timestamp:         t1,
			CPUUsageSeconds:   102.0, // +2.0s in 2.0s = 1.0 core
			HasBalloonStats:   true,
			MemoryDomainBytes: 4 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 3 * 1024 * 1024 * 1024,
			NetRxBytesTotal:   12000, // +2000B in 2.0s = 1000B/s
			NetTxBytesTotal:   5000,
		},
	})

	vm, _ = st.GetVM("default", "vm-1")
	if vm.CPUUsageCores != 1.0 {
		t.Errorf("expected CPUUsageCores=1.0, got %f", vm.CPUUsageCores)
	}
	if vm.CPUSaturationPercent != 50.0 {
		t.Errorf("expected CPUSaturationPercent=50.0%% (1 core / 2 allotted), got %f%%", vm.CPUSaturationPercent)
	}
	if vm.NetRxBytesPerSec != 1000.0 {
		t.Errorf("expected NetRxBytesPerSec=1000.0, got %f", vm.NetRxBytesPerSec)
	}
	if len(vm.CPUHistory) != 1 || vm.CPUHistory[0] != 1.0 {
		t.Errorf("expected CPUHistory=[1.0], got %v", vm.CPUHistory)
	}
}

func TestStore_MigrationReset(t *testing.T) {
	st := store.NewStore(300)
	t0 := time.Now()

	// Initial on hv-01
	st.PushSamples([]collect.VMISample{
		{
			Namespace:       "default",
			Name:            "migrating-vm",
			Node:            "hv-01",
			Timestamp:       t0,
			CPUUsageSeconds: 100.0,
		},
	})

	// Migrated to hv-02 with counter reset or discontinuity
	t1 := t0.Add(2 * time.Second)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:       "default",
			Name:            "migrating-vm",
			Node:            "hv-02",
			Timestamp:       t1,
			CPUUsageSeconds: 5.0, // reset!
		},
	})

	vm, _ := st.GetVM("default", "migrating-vm")
	if vm.Node != "hv-02" {
		t.Errorf("expected node to update to hv-02, got %s", vm.Node)
	}
	if vm.CPUUsageCores != 0 {
		t.Errorf("expected migration delta to be dropped (rate=0), got %f", vm.CPUUsageCores)
	}
}

func TestStore_AggregationsAndOvercommit(t *testing.T) {
	st := store.NewStore(300)

	// Set node allocatable memory: 10GB for hv-01, 10GB for hv-02
	const tenGB = 10 * 1024 * 1024 * 1024
	st.SetNodeAllocatable("hv-01", tenGB)
	st.SetNodeAllocatable("hv-02", tenGB)

	t0 := time.Now()

	// Push 2 VMs on hv-01 (total allocated memory 12GB -> 120% overcommit)
	// and 1 VM on hv-02 (prod namespace)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "vm-a",
			Node:              "hv-01",
			Timestamp:         t0,
			CPUUsageSeconds:   10.0,
			HasBalloonStats:   true,
			MemoryDomainBytes: 8 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 6 * 1024 * 1024 * 1024,
		},
		{
			Namespace:         "default",
			Name:              "vm-b",
			Node:              "hv-01",
			Timestamp:         t0,
			CPUUsageSeconds:   20.0,
			HasBalloonStats:   true,
			MemoryDomainBytes: 4 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 2 * 1024 * 1024 * 1024,
		},
		{
			Namespace:         "prod",
			Name:              "vm-c",
			Node:              "hv-02",
			Timestamp:         t0,
			CPUUsageSeconds:   30.0,
			HasBalloonStats:   false,
			MemoryDomainBytes: 2 * 1024 * 1024 * 1024,
		},
	})

	// Full cluster snapshot
	snapAll := st.Snapshot("")
	if len(snapAll.VMs) != 3 {
		t.Fatalf("expected 3 VMs, got %d", len(snapAll.VMs))
	}
	if len(snapAll.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(snapAll.Nodes))
	}

	// Verify hv-01 overcommit: 12GB allocated / 10GB allocatable = 1.2 (120%)
	var hv01Node *store.NodeAggregate
	for i := range snapAll.Nodes {
		if snapAll.Nodes[i].NodeName == "hv-01" {
			hv01Node = &snapAll.Nodes[i]
			break
		}
	}
	if hv01Node == nil {
		t.Fatalf("node hv-01 not found in snapshot")
	}
	if hv01Node.VMCount != 2 {
		t.Errorf("expected 2 VMs on hv-01, got %d", hv01Node.VMCount)
	}
	if hv01Node.OvercommitRatio < 1.19 || hv01Node.OvercommitRatio > 1.21 {
		t.Errorf("expected overcommit ~1.2, got %f", hv01Node.OvercommitRatio)
	}

	// Scoped snapshot to "prod" namespace:
	// - VMs should be 1
	// - Node bars should STILL BE 2 (cluster-wide)!
	snapProd := st.Snapshot("prod")
	if len(snapProd.VMs) != 1 || snapProd.VMs[0].Name != "vm-c" {
		t.Fatalf("expected 1 VM 'vm-c' in prod snapshot, got %v", snapProd.VMs)
	}
	if len(snapProd.Nodes) != 2 {
		t.Fatalf("expected 2 nodes still present in scoped snapshot, got %d", len(snapProd.Nodes))
	}
	if snapProd.ClusterTotals.TotalVMs != 1 {
		t.Errorf("expected scoped cluster total VMs = 1, got %d", snapProd.ClusterTotals.TotalVMs)
	}
}

func TestStore_ReplayIntegration(t *testing.T) {
	st := store.NewStore(300)

	// Load VMI metadata from testdata/
	vmis, err := collect.LoadReplayVMIs("../../testdata")
	if err != nil {
		t.Fatalf("failed to load replay VMIs: %v", err)
	}
	if len(vmis) == 0 {
		t.Fatalf("expected non-empty VMIs from testdata")
	}
	st.UpdateVMIs(vmis)

	collector, err := collect.NewReplayCollector("../../testdata", 2*time.Second)
	if err != nil {
		t.Fatalf("failed to create replay collector: %v", err)
	}

	ctx := context.Background()

	// Tick 1
	samples1, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("replay collect tick 1 failed: %v", err)
	}
	st.PushSamples(samples1)

	// Tick 2
	samples2, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("replay collect tick 2 failed: %v", err)
	}
	st.PushSamples(samples2)

	snap := st.Snapshot("")
	if len(snap.VMs) != 6 {
		t.Fatalf("expected 6 VMs in snapshot, got %d", len(snap.VMs))
	}

	// Verify all VMs have non-zero CPU rates on tick 2
	for _, vm := range snap.VMs {
		if vm.CPUUsageCores <= 0 {
			t.Errorf("expected positive CPUUsageCores on tick 2 for %s, got %f", vm.Name, vm.CPUUsageCores)
		}
		if len(vm.CPUHistory) != 1 {
			t.Errorf("expected 1 CPUHistory point for %s, got %d", vm.Name, len(vm.CPUHistory))
		}
	}
}
