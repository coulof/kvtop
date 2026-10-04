package query_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
)

func TestSummaryMath(t *testing.T) {
	now := time.Now()
	points := []store.Point{
		{Timestamp: now.Add(-10 * time.Second), Value: 1.0},
		{Timestamp: now.Add(-8 * time.Second), Value: 2.0},
		{Timestamp: now.Add(-6 * time.Second), Value: 3.0},
		{Timestamp: now.Add(-4 * time.Second), Value: 4.0},
		{Timestamp: now.Add(-2 * time.Second), Value: 5.0},
	}

	cutoff := now.Add(-15 * time.Second)
	summary := query.ComputeSummary(points, cutoff)
	if summary == nil {
		t.Fatalf("expected non-nil summary")
	}

	if summary.Min != 1.0 {
		t.Errorf("expected min 1.0, got %f", summary.Min)
	}
	if summary.Max != 5.0 {
		t.Errorf("expected max 5.0, got %f", summary.Max)
	}
	if summary.Avg != 3.0 {
		t.Errorf("expected avg 3.0, got %f", summary.Avg)
	}
	if summary.Last != 5.0 {
		t.Errorf("expected last 5.0, got %f", summary.Last)
	}
	if summary.P95 != 5.0 {
		t.Errorf("expected p95 5.0, got %f", summary.P95)
	}

	// Test saturation summary scaling
	sat := query.ComputeSaturationSummary(summary, 2)
	if sat == nil {
		t.Fatalf("expected non-nil saturation summary")
	}
	if sat.Last != 250.0 { // (5.0 / 2) * 100
		t.Errorf("expected last saturation 250.0, got %f", sat.Last)
	}
	if sat.Avg != 150.0 { // (3.0 / 2) * 100
		t.Errorf("expected avg saturation 150.0, got %f", sat.Avg)
	}

	// Test percent summary scaling
	pct := query.ComputePercentSummary(summary, 10)
	if pct == nil {
		t.Fatalf("expected non-nil percent summary")
	}
	if pct.Last != 50.0 { // (5.0 / 10) * 100
		t.Errorf("expected last percent 50.0, got %f", pct.Last)
	}
}

func setupTestStore(t *testing.T) (*store.Store, *query.Engine) {
	st := store.NewStore(300)
	testdataDir := filepath.Join("..", "..", "testdata")

	vmis, err := collect.LoadReplayVMIs(testdataDir)
	if err != nil {
		t.Fatalf("failed to load replay VMIs: %v", err)
	}
	st.UpdateVMIs(vmis)

	// Add nodes
	for _, n := range []string{"hv-01", "hv-02", "hv-03", "hv-04"} {
		st.OnNodeUpdated(kube.NodeInfo{
			Name:            n,
			AllocatableMem:  96 * 1024 * 1024 * 1024,
			AllocatableCPUs: 16,
			Ready:           true,
		})
	}

	// Feed samples from replay collector
	rc, err := collect.NewReplayCollector(testdataDir, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to create replay collector: %v", err)
	}

	ctx := context.Background()
	// Push 3 rounds of samples to warm up rates and history
	for i := 0; i < 3; i++ {
		samples, err := rc.Collect(ctx)
		if err != nil {
			t.Fatalf("failed collecting replay samples: %v", err)
		}
		st.PushSamples(samples)
	}

	engine := query.NewEngine(st, nil, testdataDir)
	return st, engine
}

func TestQueryTop(t *testing.T) {
	_, engine := setupTestStore(t)
	ctx := context.Background()

	// 1. Basic query
	res, err := engine.QueryTop(ctx, query.TopOptions{
		SortBy: "cpu",
		Limit:  3,
		Window: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("QueryTop failed: %v", err)
	}

	if res.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, res.Schema)
	}
	if res.WindowSeconds != 15 {
		t.Errorf("expected window 15s, got %f", res.WindowSeconds)
	}
	if res.Total < 4 {
		t.Errorf("expected at least 4 total VMs, got %d", res.Total)
	}
	if len(res.Items) != 3 {
		t.Errorf("expected 3 items capped by limit, got %d", len(res.Items))
	}
	if !res.Truncated {
		t.Errorf("expected truncated to be true")
	}

	// Verify sources and units
	first := res.Items[0]
	if first.CPUCoresUsedSource != query.SourceVirtHandler {
		t.Errorf("expected cpu source %s, got %s", query.SourceVirtHandler, first.CPUCoresUsedSource)
	}
	if first.NetRxBytesPerSecSource != query.SourceVirtHandler {
		t.Errorf("expected net rx source %s, got %s", query.SourceVirtHandler, first.NetRxBytesPerSecSource)
	}

	// 2. Namespace filtering
	nsRes, err := engine.QueryTop(ctx, query.TopOptions{
		Namespaces: []string{"default"},
		Limit:      10,
		Window:     15 * time.Second,
	})
	if err != nil {
		t.Fatalf("QueryTop with ns filter failed: %v", err)
	}
	for _, it := range nsRes.Items {
		if it.Namespace != "default" {
			t.Errorf("expected namespace default, got %s", it.Namespace)
		}
	}

	// 3. Node filtering
	nodeRes, err := engine.QueryTop(ctx, query.TopOptions{
		Node:   "hv-04",
		Limit:  10,
		Window: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("QueryTop with node filter failed: %v", err)
	}
	for _, it := range nodeRes.Items {
		if it.Node != "hv-04" {
			t.Errorf("expected node hv-04, got %s", it.Node)
		}
	}

	// 4. Window minimum enforcement
	_, err = engine.QueryTop(ctx, query.TopOptions{
		Window: 5 * time.Second,
	})
	if err != query.ErrWindowTooSmall {
		t.Errorf("expected ErrWindowTooSmall, got %v", err)
	}
}

func TestQueryVM(t *testing.T) {
	_, engine := setupTestStore(t)
	ctx := context.Background()

	// Query existing VM
	res, err := engine.QueryVM(ctx, "default", "coriolis-win-minion", query.VMOptions{
		Window:         15 * time.Second,
		IncludeSamples: true,
	})
	if err != nil {
		t.Fatalf("QueryVM failed: %v", err)
	}

	vm := res.VM
	if vm.Name != "coriolis-win-minion" {
		t.Errorf("expected name coriolis-win-minion, got %s", vm.Name)
	}
	if vm.Context == nil {
		t.Fatalf("expected non-nil VM context")
	}
	if vm.Context.CPUTopology.Cores != 4 {
		t.Errorf("expected 4 CPU cores in topology, got %d", vm.Context.CPUTopology.Cores)
	}
	if vm.Samples == nil {
		t.Errorf("expected non-nil samples when IncludeSamples: true")
	}

	// Query non-existent VM
	_, err = engine.QueryVM(ctx, "default", "non-existent-vm", query.VMOptions{
		Window: 15 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected error for non-existent VM")
	}
}

func TestQueryNodes(t *testing.T) {
	_, engine := setupTestStore(t)
	ctx := context.Background()

	res, err := engine.QueryNodes(ctx, query.NodesOptions{
		Window: 15 * time.Second,
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("QueryNodes failed: %v", err)
	}

	if res.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, res.Schema)
	}
	if res.Total != 4 {
		t.Errorf("expected 4 nodes, got %d", res.Total)
	}
	if len(res.Items) != 2 {
		t.Errorf("expected 2 items capped by limit, got %d", len(res.Items))
	}
	if !res.Truncated {
		t.Errorf("expected truncated to be true")
	}

	// Verify CPUAllocatableCores matches physical node allocatable cores
	for _, n := range res.Items {
		if n.CPUAllocatableCores != 16 {
			t.Errorf("expected 16 allocatable CPU cores for node %s, got %d", n.NodeName, n.CPUAllocatableCores)
		}
	}
}

func TestSortVMItems_DeterministicTieBreaker(t *testing.T) {
	st := store.NewStore(300)
	// Add three VMs with identical 0 usage in different alphabetical order
	st.UpdateVMIs([]kube.VMIInfo{
		{Namespace: "default", Name: "vm-z", CPUCores: 2},
		{Namespace: "default", Name: "vm-a", CPUCores: 2},
		{Namespace: "custom", Name: "vm-m", CPUCores: 2},
	})

	engine := query.NewEngine(st, nil, "")
	res, err := engine.QueryTop(context.Background(), query.TopOptions{
		SortBy: "cpu",
		Limit:  10,
		Window: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("QueryTop failed: %v", err)
	}

	if len(res.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(res.Items))
	}
	// Expected alphabetical tie-breaker: custom/vm-m, default/vm-a, default/vm-z
	if res.Items[0].Namespace != "custom" || res.Items[0].Name != "vm-m" {
		t.Errorf("expected custom/vm-m first, got %s/%s", res.Items[0].Namespace, res.Items[0].Name)
	}
	if res.Items[1].Namespace != "default" || res.Items[1].Name != "vm-a" {
		t.Errorf("expected default/vm-a second, got %s/%s", res.Items[1].Namespace, res.Items[1].Name)
	}
	if res.Items[2].Namespace != "default" || res.Items[2].Name != "vm-z" {
		t.Errorf("expected default/vm-z third, got %s/%s", res.Items[2].Namespace, res.Items[2].Name)
	}
}

func TestSnapshotDelegation(t *testing.T) {
	st, engine := setupTestStore(t)

	directSnap := st.Snapshot("")
	engineSnap := engine.Snapshot("")

	if len(directSnap.VMs) != len(engineSnap.VMs) {
		t.Errorf("snapshot VM count mismatch: %d vs %d", len(directSnap.VMs), len(engineSnap.VMs))
	}
	if len(directSnap.Nodes) != len(engineSnap.Nodes) {
		t.Errorf("snapshot Node count mismatch: %d vs %d", len(directSnap.Nodes), len(engineSnap.Nodes))
	}
}
