package store_test

import (
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/store"
)

func TestUintRateTracker_CacheHitHoldAndDecay(t *testing.T) {
	var tr store.UintRateTracker
	t0 := time.Now()
	const cacheWindow = 6.0

	// Tick 0 (t=0s): Initial counter = 1000
	rate0 := tr.Update(1000, t0, cacheWindow)
	if rate0 != 0 {
		t.Errorf("expected initial tick rate 0, got %f", rate0)
	}

	// Tick 1 (t=2s): Counter advances to 1200 (+200 bytes in 2s -> 100 B/s)
	t1 := t0.Add(2 * time.Second)
	rate1 := tr.Update(1200, t1, cacheWindow)
	if rate1 != 100.0 {
		t.Errorf("expected rate 100.0 B/s on tick 1, got %f", rate1)
	}

	// Tick 2 (t=4s): virt-handler 5s cache hit! Counter still 1200.
	// Crucial check: Rate MUST hold at 100.0 B/s rather than dropping to 0!
	t2 := t0.Add(4 * time.Second)
	rate2 := tr.Update(1200, t2, cacheWindow)
	if rate2 != 100.0 {
		t.Errorf("expected rate to hold at 100.0 B/s during cache hit, got %f", rate2)
	}

	// Tick 3 (t=5.5s): Still within 6s cache window, counter still 1200
	t3 := t0.Add(5500 * time.Millisecond)
	rate3 := tr.Update(1200, t3, cacheWindow)
	if rate3 != 100.0 {
		t.Errorf("expected rate to hold at 100.0 B/s at t=5.5s, got %f", rate3)
	}

	// Tick 4 (t=9s): Idle beyond 6s cache window (counter still 1200) -> decays to 0
	t4 := t0.Add(9 * time.Second)
	rate4 := tr.Update(1200, t4, cacheWindow)
	if rate4 != 0 {
		t.Errorf("expected idle rate to decay to 0 B/s beyond cache window, got %f", rate4)
	}

	// Tick 5 (t=11s): Counter advances to 1500 (+300 bytes over 2s from last time)
	t5 := t4.Add(2 * time.Second)
	rate5 := tr.Update(1500, t5, cacheWindow)
	if rate5 != 150.0 {
		t.Errorf("expected rate 150.0 B/s, got %f", rate5)
	}

	// Tick 6 (t=13s): Counter reset (VM reboot: 1500 -> 200)
	t6 := t5.Add(2 * time.Second)
	rate6 := tr.Update(200, t6, cacheWindow)
	if rate6 != 0 {
		t.Errorf("expected counter reset rate to be 0 (no spike), got %f", rate6)
	}
}

func TestStore_ZeroFlappingDuringVirtHandlerCacheHits(t *testing.T) {
	st := store.NewStore(300)
	t0 := time.Now()

	// Initial scrape at t=0
	st.PushSamples([]collect.VMISample{
		{
			Namespace:       "default",
			Name:            "web-vm",
			Node:            "hv-01",
			Timestamp:       t0,
			CPUUsageSeconds: 100.0,
			NetRxBytesTotal: 10000,
		},
	})

	// Scrape 1 at t=2s: counter advances
	t1 := t0.Add(2 * time.Second)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:       "default",
			Name:            "web-vm",
			Node:            "hv-01",
			Timestamp:       t1,
			CPUUsageSeconds: 102.0, // +2s over 2s = 1.0 core
			NetRxBytesTotal: 11000, // +1000B over 2s = 500 B/s
		},
	})

	vm1, _ := st.GetVM("default", "web-vm")
	if vm1.CPUUsageCores != 1.0 || vm1.NetRxBytesPerSec != 500.0 {
		t.Fatalf("expected 1.0 core and 500 B/s at t=2s, got %f cores, %f B/s", vm1.CPUUsageCores, vm1.NetRxBytesPerSec)
	}

	// Scrape 2 at t=4s: virt-handler cache hit (counters identical)
	// MUST NOT FLAP TO ZERO!
	t2 := t0.Add(4 * time.Second)
	st.PushSamples([]collect.VMISample{
		{
			Namespace:       "default",
			Name:            "web-vm",
			Node:            "hv-01",
			Timestamp:       t2,
			CPUUsageSeconds: 102.0, // same (cached)
			NetRxBytesTotal: 11000, // same (cached)
		},
	})

	vm2, _ := st.GetVM("default", "web-vm")
	if vm2.NetRxBytesPerSec != 500.0 {
		t.Errorf("FLAPPING DETECTED: expected NetRxBytesPerSec to hold at 500.0 B/s during cache hit, got %f", vm2.NetRxBytesPerSec)
	}
	if vm2.CPUUsageCores != 1.0 {
		t.Errorf("FLAPPING DETECTED: expected CPUUsageCores to hold at 1.0 during cache hit, got %f", vm2.CPUUsageCores)
	}

	// Check cluster history: should receive held values rather than 0
	snap := st.Snapshot("")
	if snap.ClusterTotals.NetRxBytesPerSec != 500.0 {
		t.Errorf("expected cluster NetRxBytesPerSec to hold at 500.0 B/s, got %f", snap.ClusterTotals.NetRxBytesPerSec)
	}
	if len(snap.ClusterTotals.NetRxHistory) < 2 {
		t.Fatalf("expected at least 2 points in cluster NetRxHistory")
	}
	lastNetRx := snap.ClusterTotals.NetRxHistory[len(snap.ClusterTotals.NetRxHistory)-1]
	if lastNetRx != 500.0 {
		t.Errorf("expected cluster history to record held rate 500.0, got %f", lastNetRx)
	}
}
