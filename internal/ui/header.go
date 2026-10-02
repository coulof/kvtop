package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/store"
)

// HeaderModel renders the top status bar.
type HeaderModel struct {
	ClusterName string
	KubeVersion string
	Interval    time.Duration
}

func (h HeaderModel) View(width int, snap store.StoreSnapshot, nodeCount int) string {
	if width < 40 {
		return TitleStyle.Render("kvtop")
	}

	cl := snap.ClusterTotals

	// Cluster identity
	clusterTag := TitleStyle.Render("kvtop") + " " + HeaderValueStyle.Render(h.ClusterName)
	if h.KubeVersion != "" {
		clusterTag += " " + HeaderLabelStyle.Render(h.KubeVersion)
	}

	// Status metrics
	nodesTotal := nodeCount
	nodesReady := len(snap.Nodes)
	if cl.NodesTotal > 0 {
		nodesTotal = cl.NodesTotal
		nodesReady = cl.NodesReady
	}
	nodesText := HeaderLabelStyle.Render("nodes ") + HeaderValueStyle.Render(fmt.Sprintf("%d/%d", nodesReady, nodesTotal))
	vmsText := HeaderLabelStyle.Render("vms ") + HeaderValueStyle.Render(fmt.Sprintf("%d/%d", cl.RunningVMs, cl.TotalVMs))

	var migText string
	if cl.MigratingVMs > 0 {
		migText = "  │  " + lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(fmt.Sprintf("⇶ %d migrating", cl.MigratingVMs))
	}

	nsLabel := "all"
	if snap.ActiveNamespace != "" {
		nsLabel = snap.ActiveNamespace
	}
	nsText := HeaderLabelStyle.Render("ns: ") + BadgeStyle.Render(nsLabel)

	intervalText := HeaderLabelStyle.Render("interval: ") + HeaderValueStyle.Render(h.Interval.String())

	content := fmt.Sprintf("%s  │  %s  │  %s%s  │  %s  │  %s",
		clusterTag,
		nodesText,
		vmsText,
		migText,
		nsText,
		intervalText,
	)

	// Wrap in a sleek border box spanning width
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(ColorMuted).
		Width(width - 2).
		Padding(0, 1)

	return boxStyle.Render(content)
}
