package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpModel renders an interactive in-app documentation and keybinding guide.
type HelpModel struct {
	ScrollY int
}

// NewHelpModel creates a help overlay model.
func NewHelpModel() *HelpModel {
	return &HelpModel{}
}

func (h *HelpModel) ScrollUp() {
	if h.ScrollY > 0 {
		h.ScrollY--
	}
}

func (h *HelpModel) ScrollDown() {
	h.ScrollY++
}

// View renders the help modal within the terminal bounds.
func (h *HelpModel) View(width, height int) string {
	innerHeight := max(1, height-2)
	textWidth := max(2, width-6)

	var sections []string

	title := TitleStyle.Render("kvtop Manual & Help") + "  " + HeaderLabelStyle.Render("Documentation: docs/manual.md")
	helpKey := HeaderLabelStyle.Render("Press [?] or [Esc] or [q] to return")
	banner := title + "\n" + helpKey + "\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", textWidth))
	sections = append(sections, banner)

	// Section: Dashboard Overview
	overviewTitle := HeaderValueStyle.Render("DASHBOARD PANELS")
	overviewBody := strings.Join([]string{
		"• Total VM CPU: Cluster-wide sum of all VM workloads (cores used / allotted vCPUs).",
		"• Nodes: Per-physical-host VM count, CPU usage, memory overcommit %, VM network, and VM storage IOPS.",
		"• Total VM Memory: Cluster-wide guest-used RAM from balloon stats with a 90% threshold line.",
		"• Total VM Network: Aggregate VM virtual interface traffic (excludes host & migration traffic).",
		"• VMs Table: Per-VM live metrics (CPU sparkline + cores/allotted, guest memory %, network, IOPS).",
	}, "\n")
	sections = append(sections, overviewTitle+"\n"+overviewBody)

	// Section: Detail View
	detailTitle := HeaderValueStyle.Render("VM DRILLDOWN (Press Enter on any VM)")
	detailBody := strings.Join([]string{
		"• Launcher RSS: Actual host memory consumed by the VM process on the hypervisor.",
		"• Per-vCPU: Live CPU utilization % sparkline, cumulative CPU time, wait time, and queue delay.",
		"• Per-NIC: Live RX/TX throughput rates, cumulative traffic, and packet error/drop counters.",
		"• Per-Disk: Live read/write IOPS and throughput, capacity, and flush request counts.",
	}, "\n")
	sections = append(sections, detailTitle+"\n"+detailBody)

	// Section: Keybindings
	keysTitle := HeaderValueStyle.Render("KEYBINDINGS")
	keysBody := strings.Join([]string{
		"  Enter      Open Detail View for selected VM (or scope table to selected node)",
		"  Esc / q    Close Detail View / help modal / cancel search filter",
		"  Tab        Toggle focus between VM Table and Nodes Panel",
		"  o          Sort by Namespace / VM Name (alphabetical default)",
		"  c / m      Sort by CPU / Guest Memory",
		"  n / d      Sort by Network throughput / Storage IOPS",
		"  s          Toggle CPU sort: absolute cores (1.80c) vs saturation (45%)",
		"  r          Reverse sort order",
		"  ← / →      Cycle sort column",
		"  /          Fuzzy search filter by VM name or namespace (Enter to apply, Esc to clear)",
		"  + / -      Increase / decrease refresh interval",
		"  ?          Toggle this Help & Documentation overlay",
		"  q          Quit kvtop",
	}, "\n")
	sections = append(sections, keysTitle+"\n"+keysBody)

	fullContent := strings.Join(sections, "\n\n")

	// Viewport scrolling
	lines := strings.Split(fullContent, "\n")
	if h.ScrollY >= len(lines) {
		h.ScrollY = max(0, len(lines)-1)
	}

	visibleEnd := min(len(lines), h.ScrollY+innerHeight)
	visibleLines := lines[h.ScrollY:visibleEnd]
	content := strings.Join(visibleLines, "\n")

	return PanelFocusedStyle.Width(width - 2).Height(innerHeight).Render(content)
}
