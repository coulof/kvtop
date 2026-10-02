package ui

import (
	"fmt"

	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui/chart"
)

// RenderCPUPanel renders the top-left CPU history panel with braille graph.
func RenderCPUPanel(width, height int, snap store.StoreSnapshot) string {
	cl := snap.ClusterTotals

	title := TitleStyle.Render("Total VM CPU")
	satPct := 0.0
	if cl.AllottedCPUs > 0 {
		satPct = (cl.CPUUsageCores / float64(cl.AllottedCPUs)) * 100
	}

	textWidth := max(2, width-4)
	textHeight := max(1, height-2)

	var header string
	var graphHeight int

	statsLine := fmt.Sprintf("%s/%s cores (%4.1f%% sat)",
		HeaderValueStyle.Render(fmt.Sprintf("%.2f", cl.CPUUsageCores)),
		HeaderLabelStyle.Render(fmt.Sprintf("%d", cl.AllottedCPUs)),
		satPct,
	)

	if textHeight >= 4 {
		header = title + "\n" + statsLine
		graphHeight = max(1, textHeight-2)
	} else {
		header = title + "  " + statsLine
		graphHeight = max(1, textHeight-1)
	}

	maxCores := float64(cl.AllottedCPUs)
	if maxCores <= 0 {
		maxCores = 1.0
	}
	for _, v := range cl.CPUHistory {
		if v > maxCores {
			maxCores = v
		}
	}

	graph := chart.RenderBrailleGraph(textWidth, graphHeight, cl.CPUHistory, maxCores, ColorPrimary)
	content := header + "\n" + graph
	return PanelStyle.Width(width - 2).Height(textHeight).Render(content)
}

// RenderMemPanel renders the guest memory history panel with braille graph and 90% threshold line.
func RenderMemPanel(width, height int, snap store.StoreSnapshot) string {
	cl := snap.ClusterTotals

	title := TitleStyle.Render("Total VM Memory (Guest)")
	memUsed := FormatBytes(float64(cl.MemoryUsedBytes))
	memAlloc := FormatBytes(float64(cl.MemoryAllocatedBytes))

	textWidth := max(2, width-4)
	textHeight := max(1, height-2)

	var header string
	var graphHeight int

	statsLine := fmt.Sprintf("%s / %s (oc:%2.0f%%)  %s",
		HeaderValueStyle.Render(memUsed),
		HeaderLabelStyle.Render(memAlloc),
		cl.OvercommitRatio*100,
		HeaderLabelStyle.Render("─ 90% limit"),
	)
	if textWidth < 36 {
		statsLine = fmt.Sprintf("%s/%s  ─ 90%%",
			HeaderValueStyle.Render(memUsed),
			HeaderLabelStyle.Render(memAlloc),
		)
	}

	if textHeight >= 4 {
		header = title + "\n" + statsLine
		graphHeight = max(1, textHeight-2)
	} else {
		header = title + "  " + statsLine
		graphHeight = max(1, textHeight-1)
	}

	maxMem := float64(cl.MemoryAllocatedBytes)
	if maxMem <= 0 {
		maxMem = float64(cl.MemoryUsedBytes) * 1.2
	}
	if maxMem <= 0 {
		maxMem = 1024 * 1024 * 1024
	}

	// 90% threshold line rendered on the memory graph
	graph := chart.RenderBrailleGraphWithThreshold(textWidth, graphHeight, cl.MemHistory, maxMem, 0.90, ColorSecondary)
	content := header + "\n" + graph
	return PanelStyle.Width(width - 2).Height(textHeight).Render(content)
}

// RenderNetPanel renders the aggregate VM network throughput panel with braille graph.
func RenderNetPanel(width, height int, snap store.StoreSnapshot) string {
	cl := snap.ClusterTotals

	title := TitleStyle.Render("Total VM Network (Aggregate)")
	textWidth := max(2, width-4)
	textHeight := max(1, height-2)

	var header string
	var graphHeight int

	statsLine := fmt.Sprintf("RX: %s   TX: %s",
		HeaderValueStyle.Render(FormatRate(cl.NetRxBytesPerSec)),
		HeaderValueStyle.Render(FormatRate(cl.NetTxBytesPerSec)),
	)

	if textHeight >= 4 {
		header = title + "\n" + statsLine
		graphHeight = max(1, textHeight-2)
	} else {
		header = title + "  " + statsLine
		graphHeight = max(1, textHeight-1)
	}

	// Plot combined or RX throughput history
	var netHistory []float64
	maxRate := 1024.0 // at least 1KB/s
	for i := range cl.NetRxHistory {
		rx := cl.NetRxHistory[i]
		tx := 0.0
		if i < len(cl.NetTxHistory) {
			tx = cl.NetTxHistory[i]
		}
		total := rx + tx
		if total > maxRate {
			maxRate = total
		}
		netHistory = append(netHistory, total)
	}

	graph := chart.RenderBrailleGraph(textWidth, graphHeight, netHistory, maxRate, ColorAccent)
	content := header + "\n" + graph
	return PanelStyle.Width(width - 2).Height(textHeight).Render(content)
}
