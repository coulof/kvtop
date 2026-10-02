package store_test

import (
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/store"
)

func TestStore_LiveMigrationWorkflow(t *testing.T) {
	st := store.NewStore(300)

	// Step 1: VMI starts on hv-01
	st.OnNodeUpdated(kube.NodeInfo{Name: "hv-01", AllocatableMem: 64 * 1024 * 1024 * 1024, Ready: true})
	st.OnNodeUpdated(kube.NodeInfo{Name: "hv-04", AllocatableMem: 64 * 1024 * 1024 * 1024, Ready: true})

	st.OnVMIUpdated(kube.VMIInfo{
		Namespace: "default",
		Name:      "database-vm",
		NodeName:  "hv-01",
		Phase:     "Running",
		CPUCores:  4,
	})

	t0 := time.Now()
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "database-vm",
			Node:              "hv-01",
			Timestamp:         t0,
			CPUUsageSeconds:   500.0,
			HasBalloonStats:   true,
			MemoryDomainBytes: 8 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 4 * 1024 * 1024 * 1024,
		},
	})

	snap1 := st.Snapshot("")
	if snap1.ClusterTotals.MigratingVMs != 0 {
		t.Fatalf("expected 0 migrating VMs initially, got %d", snap1.ClusterTotals.MigratingVMs)
	}

	// Step 2: Live Migration starts: hv-01 -> hv-04
	st.OnMigrationUpdated(kube.MigrationInfo{
		Namespace:     "default",
		VMIName:       "database-vm",
		MigrationName: "mig-database-vm-xyz",
		Phase:         "Running",
		SourceNode:    "hv-01",
		TargetNode:    "hv-04",
		Active:        true,
	})

	snap2 := st.Snapshot("")
	if snap2.ClusterTotals.MigratingVMs != 1 {
		t.Fatalf("expected 1 migrating VM in cluster snapshot, got %d", snap2.ClusterTotals.MigratingVMs)
	}

	vm2, ok := st.GetVM("default", "database-vm")
	if !ok || !vm2.IsMigrating {
		t.Fatalf("expected database-vm to have IsMigrating=true")
	}
	if vm2.MigrationTargetNode != "hv-04" || vm2.MigrationSourceNode != "hv-01" {
		t.Errorf("unexpected migration nodes: %s -> %s", vm2.MigrationSourceNode, vm2.MigrationTargetNode)
	}

	// Verify node migration counters
	var nodeHv01, nodeHv04 *store.NodeAggregate
	for i := range snap2.Nodes {
		if snap2.Nodes[i].NodeName == "hv-01" {
			nodeHv01 = &snap2.Nodes[i]
		}
		if snap2.Nodes[i].NodeName == "hv-04" {
			nodeHv04 = &snap2.Nodes[i]
		}
	}
	if nodeHv01 == nil || nodeHv01.MigratingOutCount != 1 {
		t.Errorf("expected hv-01 MigratingOutCount=1, got %v", nodeHv01)
	}
	if nodeHv04 == nil || nodeHv04.MigratingInCount != 1 {
		t.Errorf("expected hv-04 MigratingInCount=1, got %v", nodeHv04)
	}

	// Step 3: Migration completes! VMI informer reports node move to hv-04
	st.OnVMIUpdated(kube.VMIInfo{
		Namespace: "default",
		Name:      "database-vm",
		NodeName:  "hv-04",
		Phase:     "Running",
		CPUCores:  4,
	})

	snap3 := st.Snapshot("")
	if snap3.ClusterTotals.MigratingVMs != 0 {
		t.Fatalf("expected 0 migrating VMs after move to target node, got %d", snap3.ClusterTotals.MigratingVMs)
	}

	vm3, _ := st.GetVM("default", "database-vm")
	if vm3.Node != "hv-04" {
		t.Errorf("expected VM node to be hv-04, got %s", vm3.Node)
	}
	if vm3.IsMigrating {
		t.Errorf("expected IsMigrating to be false after completion")
	}

	// Step 4: Next scrape on hv-04 arrives with counter discontinuity
	// Verify that counter reset / node change protection drops the delta
	t1 := t0.Add(2 * time.Second)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "database-vm",
			Node:              "hv-04",
			Timestamp:         t1,
			CPUUsageSeconds:   10.0, // discontinuous counter on new node
			HasBalloonStats:   true,
			MemoryDomainBytes: 8 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 4 * 1024 * 1024 * 1024,
		},
	})

	vm4, _ := st.GetVM("default", "database-vm")
	if vm4.CPUUsageCores != 0 {
		t.Errorf("expected CPUUsageCores=0 after node move discontinuity (no spike), got %f", vm4.CPUUsageCores)
	}
}

func TestStore_NodeAndNamespaceInformerEvents(t *testing.T) {
	st := store.NewStore(300)

	// Node updates
	st.OnNodeUpdated(kube.NodeInfo{
		Name:            "hv-01",
		AllocatableMem:  32 * 1024 * 1024 * 1024,
		AllocatableCPUs: 16,
		Ready:           true,
	})
	st.OnNodeUpdated(kube.NodeInfo{
		Name:            "hv-02",
		AllocatableMem:  32 * 1024 * 1024 * 1024,
		AllocatableCPUs: 16,
		Ready:           false, // not ready node
	})

	// Namespace updates
	st.OnNamespaceUpdated("kube-system")
	st.OnNamespaceUpdated("default")
	st.OnNamespaceUpdated("cattle-system")

	snap := st.Snapshot("")
	if snap.ClusterTotals.NodesTotal != 2 {
		t.Errorf("expected 2 total nodes, got %d", snap.ClusterTotals.NodesTotal)
	}
	if snap.ClusterTotals.NodesReady != 1 {
		t.Errorf("expected 1 ready node, got %d", snap.ClusterTotals.NodesReady)
	}

	if len(snap.KnownNamespaces) != 3 {
		t.Fatalf("expected 3 known namespaces, got %d", len(snap.KnownNamespaces))
	}
	if snap.KnownNamespaces[0] != "cattle-system" || snap.KnownNamespaces[1] != "default" || snap.KnownNamespaces[2] != "kube-system" {
		t.Errorf("unexpected sorted namespaces: %v", snap.KnownNamespaces)
	}

	// Delete namespace
	st.OnNamespaceDeleted("cattle-system")
	snapAfter := st.Snapshot("")
	if len(snapAfter.KnownNamespaces) != 2 {
		t.Errorf("expected 2 namespaces after deletion, got %d", len(snapAfter.KnownNamespaces))
	}
}
