package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/store"
)

// NodesModel renders the per-node CPU and Memory overcommit bars.
type NodesModel struct {
	Focused       bool
	SelectedIndex int
	ScopedNode    string
}

func (m *NodesModel) MoveUp() {
	if m.SelectedIndex > 0 {
		m.SelectedIndex--
	}
}

func (m *NodesModel) MoveDown(total int) {
	if m.SelectedIndex < total-1 {
		m.SelectedIndex++
	}
}

func (m *NodesModel) ToggleScope(nodes []store.NodeAggregate) {
	if len(nodes) == 0 {
		return
	}
	if m.SelectedIndex >= len(nodes) {
		m.SelectedIndex = 0
	}
	target := nodes[m.SelectedIndex].NodeName
	if m.ScopedNode == target {
		m.ScopedNode = "" // clear scope
	} else {
		m.ScopedNode = target
	}
}

func (m NodesModel) View(width, height int, nodes []store.NodeAggregate) string {
	innerWidth := max(2, width-4)
	innerHeight := max(1, height-2)

	var lines []string

	title := "Nodes (Physical Hosts)"
	if m.ScopedNode != "" {
		title += fmt.Sprintf(" [Scoped: %s]", m.ScopedNode)
	}

	titleStyled := TitleStyle.Render(title)
	if m.Focused {
		titleStyled = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(title + " (Focused - [↑/↓] select, [Enter] filter)")
	}
	lines = append(lines, titleStyled)

	if len(nodes) == 0 {
		lines = append(lines, HeaderLabelStyle.Render("No nodes found"))
		content := strings.Join(lines, "\n")
		style := PanelStyle
		if m.Focused {
			style = PanelFocusedStyle
		}
		return style.Width(width - 2).Height(innerHeight).Render(content)
	}

	hostW, vmsW, cpuW, memW, rxW, txW, diskW := nodeColumnWidths(innerWidth)

	header := strings.Join([]string{
		padRight("HOST", hostW),
		padRight("VMS", vmsW),
		padRight("CPU", cpuW),
		padRight("MEM (OVERCOMMIT)", memW),
		padRight("RX", rxW),
		padRight("TX", txW),
		padRight("IOPS", diskW),
	}, " ")

	totalW := hostW + 1 + vmsW + 1 + cpuW + 1 + memW + 1 + rxW + 1 + txW + 1 + diskW
	if innerWidth > totalW {
		header += strings.Repeat(" ", innerWidth-totalW)
	}

	headerView := TableHeaderStyle.Width(innerWidth).Render(header)
	lines = append(lines, headerView)

	maxVMs := 0
	for _, n := range nodes {
		if n.VMCount > maxVMs {
			maxVMs = n.VMCount
		}
	}
	if maxVMs == 0 {
		maxVMs = 1
	}

	barWidth := 4
	if cpuW > 13 {
		barWidth = 5
	}

	vmsBarWidth := 3
	if vmsW >= 9 {
		vmsBarWidth = 4
	}
	if vmsW >= 10 {
		vmsBarWidth = 5
	}

	for i, n := range nodes {
		selected := m.Focused && i == m.SelectedIndex

		var bg lipgloss.TerminalColor
		rowStyle := TableRowStyle
		if selected {
			bg = ColorHighlight
			rowStyle = TableRowSelectedStyle
		}

		prefix := "  "
		if selected {
			prefix = "▶ "
		} else if m.ScopedNode == n.NodeName {
			prefix = "● "
		}

		nameText := prefix + truncate(n.NodeName, hostW-2)
		if m.ScopedNode == n.NodeName && !selected {
			nameText = BadgeActiveStyle.Render(prefix + truncate(n.NodeName, hostW-2))
		}

		vmTag := fmt.Sprintf("%dVM", n.VMCount)
		if n.MigratingInCount > 0 || n.MigratingOutCount > 0 {
			vmTag = fmt.Sprintf("%d⇶", n.VMCount)
		}

		vmsPct := (float64(n.VMCount) / float64(maxVMs)) * 100.0
		barCol := ColorSecondary
		if n.MigratingInCount > 0 || n.MigratingOutCount > 0 {
			barCol = ColorAccent
		}
		vmsBar := RenderColoredBarWithBg(vmsBarWidth, vmsPct, barCol, bg)
		cVmsVal := padRight(vmTag, max(1, vmsW-(vmsBarWidth+1)))

		cpuCores := n.CPUUsageCores
		var cpuSat float64
		if n.AllottedCPUs > 0 {
			cpuSat = (cpuCores / float64(n.AllottedCPUs)) * 100
		}
		cpuBar := RenderBarWithBg(barWidth, cpuSat, bg)
		cpuVal := fmt.Sprintf("%4.1fc", cpuCores)
		cCpuVal := padRight(cpuVal, max(1, cpuW-(barWidth+1)))

		ocPct := n.OvercommitRatio * 100
		memBar := RenderBarWithBg(barWidth, ocPct, bg)
		memVal := fmt.Sprintf("%3.0f%% oc", ocPct)
		cMemVal := padRight(memVal, max(1, memW-(barWidth+1)))

		rxText := FormatRate(n.NetRxBytesPerSec)
		txText := FormatRate(n.NetTxBytesPerSec)
		iopsText := fmt.Sprintf("%4.1f", n.StorageTotalIOPS)

		cHost := padRight(nameText, hostW)
		cRx := padRight("▼"+rxText, rxW)
		cTx := padRight("▲"+txText, txW)
		cIops := padRight(iopsText, diskW)

		if selected {
			line := rowStyle.Render(cHost) +
				rowStyle.Render(" ") +
				vmsBar + rowStyle.Render(" "+cVmsVal) +
				rowStyle.Render(" ") +
				cpuBar + rowStyle.Render(" "+cCpuVal) +
				rowStyle.Render(" ") +
				memBar + rowStyle.Render(" "+cMemVal) +
				rowStyle.Render(" ") +
				rowStyle.Render(cRx) +
				rowStyle.Render(" ") +
				rowStyle.Render(cTx) +
				rowStyle.Render(" ") +
				rowStyle.Render(cIops)

			if innerWidth > totalW {
				line += rowStyle.Render(strings.Repeat(" ", innerWidth-totalW))
			}
			lines = append(lines, line)
		} else {
			line := strings.Join([]string{
				cHost,
				vmsBar + " " + cVmsVal,
				cpuBar + " " + cCpuVal,
				memBar + " " + cMemVal,
				cRx,
				cTx,
				cIops,
			}, " ")
			if innerWidth > totalW {
				line += strings.Repeat(" ", innerWidth-totalW)
			}
			lines = append(lines, TableRowStyle.Render(line))
		}
	}

	content := strings.Join(lines, "\n")

	style := PanelStyle
	if m.Focused {
		style = PanelFocusedStyle
	}

	return style.Width(width - 2).Height(innerHeight).Render(content)
}

func nodeColumnWidths(innerWidth int) (hostW, vmsW, cpuW, memW, rxW, txW, diskW int) {
	if innerWidth < 70 {
		return 8, 8, 11, 13, 7, 7, 5
	}
	if innerWidth < 85 {
		return 9, 9, 12, 14, 8, 8, 5
	}
	hostW = 10
	vmsW = 10
	cpuW = 14
	memW = 16
	rxW = 9
	txW = 9
	diskW = 6

	if innerWidth >= 95 {
		extra := min(innerWidth-80, 4)
		hostW += extra
	}
	return
}
