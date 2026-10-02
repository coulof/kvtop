package ui_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui"
)

func sampleVMs() []store.VMSnapshot {
	return []store.VMSnapshot{
		{
			Namespace:            "default",
			Name:                 "vm-alpha",
			Node:                 "hv-01",
			IP:                   "10.0.0.1",
			AllottedCPUs:         2,
			CPUUsageCores:        1.2,
			CPUSaturationPercent: 60.0,
			HasBalloonStats:      true,
			MemoryUsedBytes:      2 * 1024 * 1024 * 1024,
			MemoryTotalBytes:     4 * 1024 * 1024 * 1024,
			MemoryUsedPercent:    50.0,
			NetRxBytesPerSec:     1000,
			NetTxBytesPerSec:     500,
			StorageTotalIOPS:     25.0,
		},
		{
			Namespace:            "default",
			Name:                 "vm-beta",
			Node:                 "hv-02",
			IP:                   "10.0.0.2",
			AllottedCPUs:         4,
			CPUUsageCores:        1.8,
			CPUSaturationPercent: 45.0,
			HasBalloonStats:      false,
			MemoryUsedBytes:      0,
			MemoryTotalBytes:     8 * 1024 * 1024 * 1024,
			MemoryUsedPercent:    0,
			NetRxBytesPerSec:     5000,
			NetTxBytesPerSec:     2000,
			StorageTotalIOPS:     10.0,
		},
		{
			Namespace:            "kube-system",
			Name:                 "vm-gamma",
			Node:                 "hv-01",
			IP:                   "10.0.0.3",
			AllottedCPUs:         1,
			CPUUsageCores:        0.5,
			CPUSaturationPercent: 50.0,
			HasBalloonStats:      true,
			MemoryUsedBytes:      3 * 1024 * 1024 * 1024,
			MemoryTotalBytes:     4 * 1024 * 1024 * 1024,
			MemoryUsedPercent:    75.0,
			NetRxBytesPerSec:     500,
			NetTxBytesPerSec:     100,
			StorageTotalIOPS:     50.0,
		},
	}
}

func TestTableModel_SortByCPU(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()

	// Default sort: absolute CPU cores descending -> beta (1.8), alpha (1.2), gamma (0.5)
	tm.SortColumn = "cpu"
	tm.SortSaturation = false
	sorted := tm.FilterAndSort(vms)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 VMs, got %d", len(sorted))
	}
	if sorted[0].Name != "vm-beta" || sorted[1].Name != "vm-alpha" || sorted[2].Name != "vm-gamma" {
		t.Errorf("unexpected sort order by absolute CPU: %s, %s, %s", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}

	// Toggle saturation: alpha (60%), gamma (50%), beta (45%)
	tm.ToggleCPUSort()
	sortedSat := tm.FilterAndSort(vms)
	if sortedSat[0].Name != "vm-alpha" || sortedSat[1].Name != "vm-gamma" || sortedSat[2].Name != "vm-beta" {
		t.Errorf("unexpected sort order by saturation: %s, %s, %s", sortedSat[0].Name, sortedSat[1].Name, sortedSat[2].Name)
	}

	// Reverse sort
	tm.ToggleReverse()
	sortedRev := tm.FilterAndSort(vms)
	if sortedRev[0].Name != "vm-beta" || sortedRev[2].Name != "vm-alpha" {
		t.Errorf("unexpected reverse sort order: %s, %s, %s", sortedRev[0].Name, sortedRev[1].Name, sortedRev[2].Name)
	}
}

func TestTableModel_SortByMemory(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()
	tm.SortColumn = "mem"

	// gamma (75%), alpha (50%), then un-ballooned beta
	sorted := tm.FilterAndSort(vms)
	if sorted[0].Name != "vm-gamma" || sorted[1].Name != "vm-alpha" || sorted[2].Name != "vm-beta" {
		t.Errorf("unexpected sort order by memory: %s, %s, %s", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
}

func TestTableModel_SortByNetAndDisk(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()

	// RX: beta (5000), alpha (1000), gamma (500)
	tm.SortColumn = "rx"
	sortedRx := tm.FilterAndSort(vms)
	if sortedRx[0].Name != "vm-beta" || sortedRx[1].Name != "vm-alpha" || sortedRx[2].Name != "vm-gamma" {
		t.Errorf("unexpected sort order by rx: %s, %s, %s", sortedRx[0].Name, sortedRx[1].Name, sortedRx[2].Name)
	}

	// TX: beta (2000), alpha (500), gamma (100)
	tm.SortColumn = "tx"
	sortedTx := tm.FilterAndSort(vms)
	if sortedTx[0].Name != "vm-beta" || sortedTx[1].Name != "vm-alpha" || sortedTx[2].Name != "vm-gamma" {
		t.Errorf("unexpected sort order by tx: %s, %s, %s", sortedTx[0].Name, sortedTx[1].Name, sortedTx[2].Name)
	}

	// Disk: gamma (50), alpha (25), beta (10)
	tm.SortColumn = "disk"
	sortedDisk := tm.FilterAndSort(vms)
	if sortedDisk[0].Name != "vm-gamma" || sortedDisk[1].Name != "vm-alpha" || sortedDisk[2].Name != "vm-beta" {
		t.Errorf("unexpected sort order by disk: %s, %s, %s", sortedDisk[0].Name, sortedDisk[1].Name, sortedDisk[2].Name)
	}
}

func TestTableModel_FilteringAndScoping(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()

	// Text filter matching "alpha"
	tm.FilterQuery = "alpha"
	filtered := tm.FilterAndSort(vms)
	if len(filtered) != 1 || filtered[0].Name != "vm-alpha" {
		t.Errorf("expected only vm-alpha, got %v", filtered)
	}

	// Clear text filter, apply node scoping to "hv-01"
	tm.FilterQuery = ""
	tm.ScopedNode = "hv-01"
	scoped := tm.FilterAndSort(vms)
	if len(scoped) != 2 {
		t.Fatalf("expected 2 VMs on hv-01, got %d", len(scoped))
	}
	for _, vm := range scoped {
		if vm.Node != "hv-01" {
			t.Errorf("unexpected node %s for VM %s", vm.Node, vm.Name)
		}
	}
}

func TestTableModel_CursorAndNavigation(t *testing.T) {
	tm := ui.NewTableModel()
	total := 10

	if tm.Cursor != 0 {
		t.Errorf("expected initial cursor 0, got %d", tm.Cursor)
	}

	tm.MoveDown(total)
	if tm.Cursor != 1 {
		t.Errorf("expected cursor 1 after MoveDown, got %d", tm.Cursor)
	}

	tm.MoveBottom(total)
	if tm.Cursor != 9 {
		t.Errorf("expected cursor 9 after MoveBottom, got %d", tm.Cursor)
	}

	tm.MoveUp()
	if tm.Cursor != 8 {
		t.Errorf("expected cursor 8 after MoveUp, got %d", tm.Cursor)
	}

	tm.MoveTop()
	if tm.Cursor != 0 {
		t.Errorf("expected cursor 0 after MoveTop, got %d", tm.Cursor)
	}
}

func TestTableModel_SortByName(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()

	// Default sort is name: default/vm-alpha, default/vm-beta, kube-system/vm-gamma
	sorted := tm.FilterAndSort(vms)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 VMs, got %d", len(sorted))
	}
	if sorted[0].Name != "vm-alpha" || sorted[1].Name != "vm-beta" || sorted[2].Name != "vm-gamma" {
		t.Errorf("unexpected sort order by name: %s, %s, %s", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}

	// Reverse sort by name: kube-system/vm-gamma, default/vm-beta, default/vm-alpha
	tm.ToggleReverse()
	sortedRev := tm.FilterAndSort(vms)
	if sortedRev[0].Name != "vm-gamma" || sortedRev[1].Name != "vm-beta" || sortedRev[2].Name != "vm-alpha" {
		t.Errorf("unexpected reverse sort order by name: %s, %s, %s", sortedRev[0].Name, sortedRev[1].Name, sortedRev[2].Name)
	}
}

func TestTableModel_ColumnCycling(t *testing.T) {
	tm := ui.NewTableModel()
	if tm.SortColumn != "name" {
		t.Fatalf("expected initial sort name, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "cpu" {
		t.Errorf("expected next column cpu, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "mem" {
		t.Errorf("expected next column mem, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "rx" {
		t.Errorf("expected next column rx, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "tx" {
		t.Errorf("expected next column tx, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "disk" {
		t.Errorf("expected next column disk, got %s", tm.SortColumn)
	}

	tm.NextSortColumn()
	if tm.SortColumn != "name" {
		t.Errorf("expected wrap around to name, got %s", tm.SortColumn)
	}

	tm.PrevSortColumn()
	if tm.SortColumn != "disk" {
		t.Errorf("expected prev column disk, got %s", tm.SortColumn)
	}
}

func TestTableModel_View_NoArtifactsAndProperAlignment(t *testing.T) {
	vms := sampleVMs()
	tm := ui.NewTableModel()

	// Test sort by RX
	tm.SortColumn = "rx"
	width := 84
	height := 15

	rendered := tm.View(width, height, vms)
	if strings.ContainsRune(rendered, '\ufffd') {
		t.Errorf("rendered table contains UTF-8 replacement character (\ufffd)!")
	}
	if !strings.Contains(rendered, "IP") {
		t.Errorf("rendered table missing IP column header!")
	}
	if !strings.Contains(rendered, "RX") || !strings.Contains(rendered, "TX") {
		t.Errorf("rendered table missing RX or TX column header!")
	}

	lines := strings.Split(rendered, "\n")
	for i, l := range lines {
		w := lipgloss.Width(l)
		if w != width {
			t.Errorf("line %d: expected width %d, got %d (line: %q)", i, width, w, l)
		}
	}
}

func TestTableModel_View_WideTerminalAutoAdjust(t *testing.T) {
	vms := []store.VMSnapshot{
		{
			Namespace:            "harvester-system",
			Name:                 "tumbleweed-flint-florian",
			Node:                 "hv-03",
			IP:                   "192.168.122.155",
			AllottedCPUs:         4,
			CPUUsageCores:        1.2,
			CPUSaturationPercent: 30.0,
			HasBalloonStats:      true,
			MemoryUsedBytes:      2 * 1024 * 1024 * 1024,
			MemoryTotalBytes:     4 * 1024 * 1024 * 1024,
			MemoryUsedPercent:    50.0,
			NetRxBytesPerSec:     1000,
			NetTxBytesPerSec:     500,
			StorageTotalIOPS:     15.0,
		},
	}
	tm := ui.NewTableModel()

	// Test at standard wide terminal width (140)
	width := 140
	height := 10
	rendered := tm.View(width, height, vms)

	// Verify long VM name is NOT truncated on wide terminal
	if !strings.Contains(rendered, "tumbleweed-flint-florian") {
		t.Errorf("expected full VM name 'tumbleweed-flint-florian' in wide view, got: %s", rendered)
	}

	// Verify namespace is also full
	if !strings.Contains(rendered, "harvester-system") {
		t.Errorf("expected full namespace 'harvester-system' in wide view, got: %s", rendered)
	}

	// Verify exact line width consistency
	for i, l := range strings.Split(rendered, "\n") {
		w := lipgloss.Width(l)
		if w != width {
			t.Errorf("line %d: expected width %d, got %d", i, width, w)
		}
	}
}
