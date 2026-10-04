package diagnose_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
)

func setupTestDiagnoser(t *testing.T) (*store.Store, *diagnose.Diagnoser) {
	st := store.NewStore(300)
	testdataDir := filepath.Join("..", "..", "testdata")

	vmis, err := collect.LoadReplayVMIs(testdataDir)
	if err != nil {
		t.Fatalf("failed to load replay VMIs: %v", err)
	}
	st.UpdateVMIs(vmis)

	for _, n := range []string{"hv-01", "hv-02", "hv-03", "hv-04"} {
		st.OnNodeUpdated(kube.NodeInfo{
			Name:            n,
			AllocatableMem:  96 * 1024 * 1024 * 1024,
			AllocatableCPUs: 16,
			Ready:           true,
		})
	}

	rc, err := collect.NewReplayCollector(testdataDir, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to create replay collector: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		samples, err := rc.Collect(ctx)
		if err != nil {
			t.Fatalf("failed collecting replay samples: %v", err)
		}
		st.PushSamples(samples)
	}

	engine := query.NewEngine(st, nil, testdataDir)
	d := diagnose.NewDiagnoser(engine)
	return st, d
}

func TestDiagnose_ReplayCluster(t *testing.T) {
	_, d := setupTestDiagnoser(t)
	ctx := context.Background()

	res, err := d.Diagnose(ctx, diagnose.DiagnoseOptions{
		Window: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	if res.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, res.Schema)
	}
	if res.WindowSeconds != 30 {
		t.Errorf("expected window 30s, got %f", res.WindowSeconds)
	}

	// Coriolis-win-minion and win2k25-kestrel-50i have no balloon driver installed
	foundMetricsMissing := false
	for _, f := range res.Findings {
		if f.ID == diagnose.RuleMetricsMissing {
			foundMetricsMissing = true
			if f.Severity != diagnose.SeverityInfo {
				t.Errorf("expected SeverityInfo for balloon stats missing, got %s", f.Severity)
			}
		}
	}
	if !foundMetricsMissing {
		t.Errorf("expected metrics_missing finding in replay cluster")
	}
}

func TestRule_CPUSaturated(t *testing.T) {
	st := store.NewStore(300)
	st.UpdateVMIs([]kube.VMIInfo{
		{Namespace: "default", Name: "vm-busy", CPUCores: 2, Phase: "Running"},
	})

	now := time.Now()
	// Push samples with 1.9 cores used out of 2 allotted = 95% saturation
	st.PushSamples([]collect.VMISample{
		{Namespace: "default", Name: "vm-busy", Timestamp: now.Add(-6 * time.Second), CPUUsageSeconds: 10.0},
		{Namespace: "default", Name: "vm-busy", Timestamp: now.Add(-4 * time.Second), CPUUsageSeconds: 13.8},
		{Namespace: "default", Name: "vm-busy", Timestamp: now.Add(-2 * time.Second), CPUUsageSeconds: 17.6},
	})

	engine := query.NewEngine(st, nil, "")
	d := diagnose.NewDiagnoser(engine)

	res, err := d.Diagnose(context.Background(), diagnose.DiagnoseOptions{
		TargetVM: "default/vm-busy",
		Window:   15 * time.Second,
		Thresholds: diagnose.Thresholds{
			CPUSaturationRatio: 0.90,
		},
	})
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	found := false
	for _, f := range res.Findings {
		if f.ID == diagnose.RuleCPUSaturated {
			found = true
			if f.Severity != diagnose.SeverityWarning {
				t.Errorf("expected SeverityWarning for 95%% saturation, got %s", f.Severity)
			}
			if f.Subject.Name != "vm-busy" {
				t.Errorf("expected subject vm-busy, got %s", f.Subject.Name)
			}
		}
	}
	if !found {
		t.Errorf("expected cpu_saturated finding")
	}
}

func TestRule_MemGuestPressure(t *testing.T) {
	st := store.NewStore(300)
	st.UpdateVMIs([]kube.VMIInfo{
		{Namespace: "default", Name: "vm-mem-heavy", CPUCores: 2, Phase: "Running"},
	})

	now := time.Now()
	// Domain: 10GB, Usable: 500MB -> Used: 9.5GB = 95% used
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "vm-mem-heavy",
			Timestamp:         now.Add(-4 * time.Second),
			HasBalloonStats:   true,
			MemoryDomainBytes: 10 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 500 * 1024 * 1024,
		},
		{
			Namespace:         "default",
			Name:              "vm-mem-heavy",
			Timestamp:         now.Add(-2 * time.Second),
			HasBalloonStats:   true,
			MemoryDomainBytes: 10 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 500 * 1024 * 1024,
		},
	})

	engine := query.NewEngine(st, nil, "")
	d := diagnose.NewDiagnoser(engine)

	res, err := d.Diagnose(context.Background(), diagnose.DiagnoseOptions{
		TargetVM: "default/vm-mem-heavy",
		Window:   15 * time.Second,
		Thresholds: diagnose.Thresholds{
			MemGuestPressureRatio: 0.90,
		},
	})
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	found := false
	for _, f := range res.Findings {
		if f.ID == diagnose.RuleMemGuestPressure {
			found = true
			if f.Severity != diagnose.SeverityCritical {
				t.Errorf("expected SeverityCritical for >= 95%% memory pressure, got %s", f.Severity)
			}
		}
	}
	if !found {
		t.Errorf("expected mem_guest_pressure finding")
	}
}

func TestRule_NodeOvercommit(t *testing.T) {
	st := store.NewStore(300)
	// Node has 10GB allocatable memory
	st.OnNodeUpdated(kube.NodeInfo{
		Name:            "hv-small",
		AllocatableMem:  10 * 1024 * 1024 * 1024,
		AllocatableCPUs: 4,
		Ready:           true,
	})

	// VM allocated 20GB on hv-small = 200% overcommit
	st.UpdateVMIs([]kube.VMIInfo{
		{Namespace: "default", Name: "vm-big", NodeName: "hv-small", Phase: "Running"},
	})
	now := time.Now()
	st.PushSamples([]collect.VMISample{
		{
			Namespace:         "default",
			Name:              "vm-big",
			Node:              "hv-small",
			Timestamp:         now.Add(-2 * time.Second),
			HasBalloonStats:   true,
			MemoryDomainBytes: 20 * 1024 * 1024 * 1024,
			MemoryUsableBytes: 5 * 1024 * 1024 * 1024,
		},
	})

	engine := query.NewEngine(st, nil, "")
	d := diagnose.NewDiagnoser(engine)

	res, err := d.Diagnose(context.Background(), diagnose.DiagnoseOptions{
		Node:   "hv-small",
		Window: 15 * time.Second,
		Thresholds: diagnose.Thresholds{
			NodeOvercommitRatio: 1.50,
		},
	})
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	found := false
	for _, f := range res.Findings {
		if f.ID == diagnose.RuleNodeOvercommit {
			found = true
			if f.Severity != diagnose.SeverityCritical {
				t.Errorf("expected SeverityCritical for >= 2.0 overcommit ratio, got %s", f.Severity)
			}
			if f.Subject.Name != "hv-small" {
				t.Errorf("expected subject hv-small, got %s", f.Subject.Name)
			}
		}
	}
	if !found {
		t.Errorf("expected node_overcommit finding")
	}
}

func TestRule_NodeImbalance(t *testing.T) {
	st := store.NewStore(300)
	// 3 nodes: hv-01 (0.2c), hv-02 (0.2c), hv-03 (2.0c -> 10x median)
	for _, n := range []string{"hv-01", "hv-02", "hv-03"} {
		st.OnNodeUpdated(kube.NodeInfo{
			Name:            n,
			AllocatableMem:  64 * 1024 * 1024 * 1024,
			AllocatableCPUs: 16,
			Ready:           true,
		})
	}

	st.UpdateVMIs([]kube.VMIInfo{
		{Namespace: "default", Name: "vm-1", NodeName: "hv-01", CPUCores: 2, Phase: "Running"},
		{Namespace: "default", Name: "vm-2", NodeName: "hv-02", CPUCores: 2, Phase: "Running"},
		{Namespace: "default", Name: "vm-3", NodeName: "hv-03", CPUCores: 4, Phase: "Running"},
	})

	now := time.Now()
	st.PushSamples([]collect.VMISample{
		{Namespace: "default", Name: "vm-1", Node: "hv-01", Timestamp: now.Add(-4 * time.Second), CPUUsageSeconds: 10.0},
		{Namespace: "default", Name: "vm-2", Node: "hv-02", Timestamp: now.Add(-4 * time.Second), CPUUsageSeconds: 10.0},
		{Namespace: "default", Name: "vm-3", Node: "hv-03", Timestamp: now.Add(-4 * time.Second), CPUUsageSeconds: 10.0},
	})
	st.PushSamples([]collect.VMISample{
		{Namespace: "default", Name: "vm-1", Node: "hv-01", Timestamp: now.Add(-2 * time.Second), CPUUsageSeconds: 10.4}, // 0.2c
		{Namespace: "default", Name: "vm-2", Node: "hv-02", Timestamp: now.Add(-2 * time.Second), CPUUsageSeconds: 10.4}, // 0.2c
		{Namespace: "default", Name: "vm-3", Node: "hv-03", Timestamp: now.Add(-2 * time.Second), CPUUsageSeconds: 14.4}, // 2.0c
	})

	engine := query.NewEngine(st, nil, "")
	d := diagnose.NewDiagnoser(engine)

	res, err := d.Diagnose(context.Background(), diagnose.DiagnoseOptions{
		Window: 15 * time.Second,
		Thresholds: diagnose.Thresholds{
			NodeImbalanceRatio: 2.0,
		},
	})
	if err != nil {
		t.Fatalf("Diagnose failed: %v", err)
	}

	found := false
	for _, f := range res.Findings {
		if f.ID == diagnose.RuleNodeImbalance {
			found = true
			if f.Subject.Name != "hv-03" {
				t.Errorf("expected subject hv-03, got %s", f.Subject.Name)
			}
		}
	}
	if !found {
		t.Errorf("expected node_imbalance finding for hv-03")
	}
}

func TestRule_WindowValidation(t *testing.T) {
	st := store.NewStore(300)
	engine := query.NewEngine(st, nil, "")
	d := diagnose.NewDiagnoser(engine)

	_, err := d.Diagnose(context.Background(), diagnose.DiagnoseOptions{
		Window: 5 * time.Second,
	})
	if err != query.ErrWindowTooSmall {
		t.Errorf("expected ErrWindowTooSmall, got %v", err)
	}
}
