package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui"
)

func TestDetailModel_RenderingAndSections(t *testing.T) {
	vm := store.VMSnapshot{
		Namespace:    "default",
		Name:         "test-vm",
		Node:         "hv-01",
		Phase:        "Running",
		AllottedCPUs: 2,
	}

	detail := ui.NewDetailModel(vm, "virt-launcher-test-vm-12345")

	// View before stats arrive
	viewInitial := detail.View(100, 30)
	if !strings.Contains(viewInitial, "virt-launcher-test-vm-12345") {
		t.Errorf("expected view to contain pod name")
	}

	// Update with virsh stats
	stats := collect.VirshStats{
		Timestamp:           time.Now(),
		DomainName:          "default_test-vm",
		BalloonRSSBytes:     962468 * 1024,
		BalloonCurrentBytes: 4 * 1024 * 1024 * 1024,
		BalloonUsableBytes:  3 * 1024 * 1024 * 1024,
		VCPUs: []collect.VCPUStat{
			{ID: 0, State: 1, TimeNs: 15000000000, DelayNs: 2500000},
			{ID: 1, State: 1, TimeNs: 25000000000, DelayNs: 1500000},
		},
		NICs: []collect.NICStat{
			{Name: "tap-eth0", RxBytes: 250000000, TxBytes: 50000000},
		},
		Disks: []collect.DiskStat{
			{Name: "vda", CapacityBytes: 20 * 1024 * 1024 * 1024, ReadReqs: 1200, WriteReqs: 5400},
		},
	}

	detail.UpdateStats(stats)
	viewStats := detail.View(100, 30)

	if !strings.Contains(viewStats, "Launcher RSS (Host Cost)") {
		t.Errorf("expected view to contain Launcher RSS")
	}
	if !strings.Contains(viewStats, "cpu #0") || !strings.Contains(viewStats, "cpu #1") {
		t.Errorf("expected view to contain per-vCPU rows")
	}
	if !strings.Contains(viewStats, "tap-eth0") {
		t.Errorf("expected view to contain NIC name")
	}
	if !strings.Contains(viewStats, "vda") {
		t.Errorf("expected view to contain disk name")
	}

	// Test scrolling
	detail.ScrollDown()
	if detail.ScrollY != 1 {
		t.Errorf("expected ScrollY=1, got %d", detail.ScrollY)
	}
	detail.ScrollUp()
	if detail.ScrollY != 0 {
		t.Errorf("expected ScrollY=0, got %d", detail.ScrollY)
	}

	// Verify height bounds
	h := lipgloss.Height(viewStats)
	if h > 30 {
		t.Errorf("expected height <= 30, got %d", h)
	}
}
