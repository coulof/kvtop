package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui/chart"
)

var sortColumns = []string{"name", "cpu", "mem", "rx", "tx", "disk"}

// TableModel manages the VM list, sorting, filtering, and selection cursor.
type TableModel struct {
	Focused        bool
	Cursor         int
	ScrollOffset   int
	SortColumn     string
	SortReverse    bool
	SortSaturation bool
	FilterQuery    string
	ScopedNode     string
}

func NewTableModel() *TableModel {
	return &TableModel{
		Focused:     true,
		SortColumn:  "name",
		FilterQuery: "",
	}
}

func (m *TableModel) MoveUp() {
	if m.Cursor > 0 {
		m.Cursor--
		if m.Cursor < m.ScrollOffset {
			m.ScrollOffset = m.Cursor
		}
	}
}

func (m *TableModel) MoveDown(total int) {
	if total == 0 {
		return
	}
	if m.Cursor < total-1 {
		m.Cursor++
	}
}

func (m *TableModel) MoveTop() {
	m.Cursor = 0
	m.ScrollOffset = 0
}

func (m *TableModel) MoveBottom(total int) {
	if total > 0 {
		m.Cursor = total - 1
	}
}

func (m *TableModel) PrevSortColumn() {
	idx := 0
	for i, c := range sortColumns {
		if c == m.SortColumn {
			idx = i
			break
		}
	}
	idx = (idx - 1 + len(sortColumns)) % len(sortColumns)
	m.SortColumn = sortColumns[idx]
}

func (m *TableModel) NextSortColumn() {
	idx := 0
	for i, c := range sortColumns {
		if c == m.SortColumn {
			idx = i
			break
		}
	}
	idx = (idx + 1) % len(sortColumns)
	m.SortColumn = sortColumns[idx]
}

func (m *TableModel) ToggleReverse() {
	m.SortReverse = !m.SortReverse
}

func (m *TableModel) ToggleCPUSort() {
	m.SortSaturation = !m.SortSaturation
}

// FilterAndSort returns the matching VMs sorted per current configuration.
func (m *TableModel) FilterAndSort(vms []store.VMSnapshot) []store.VMSnapshot {
	var filtered []store.VMSnapshot

	for _, vm := range vms {
		// Node scope check
		if m.ScopedNode != "" && vm.Node != m.ScopedNode {
			continue
		}
		// Text filter check
		if m.FilterQuery != "" {
			q := strings.ToLower(m.FilterQuery)
			if !strings.Contains(strings.ToLower(vm.Name), q) && !strings.Contains(strings.ToLower(vm.Namespace), q) {
				continue
			}
		}
		filtered = append(filtered, vm)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if m.SortColumn == "name" {
			var less bool
			if filtered[i].Namespace != filtered[j].Namespace {
				less = filtered[i].Namespace < filtered[j].Namespace
			} else {
				less = strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
			}
			if m.SortReverse {
				return !less
			}
			return less
		}

		var less bool
		switch m.SortColumn {
		case "cpu":
			if m.SortSaturation {
				less = filtered[i].CPUSaturationPercent < filtered[j].CPUSaturationPercent
			} else {
				less = filtered[i].CPUUsageCores < filtered[j].CPUUsageCores
			}
		case "mem":
			if filtered[i].HasBalloonStats != filtered[j].HasBalloonStats {
				less = !filtered[i].HasBalloonStats
			} else {
				less = filtered[i].MemoryUsedPercent < filtered[j].MemoryUsedPercent
			}
		case "rx":
			less = filtered[i].NetRxBytesPerSec < filtered[j].NetRxBytesPerSec
		case "tx":
			less = filtered[i].NetTxBytesPerSec < filtered[j].NetTxBytesPerSec
		case "net":
			rateI := filtered[i].NetRxBytesPerSec + filtered[i].NetTxBytesPerSec
			rateJ := filtered[j].NetRxBytesPerSec + filtered[j].NetTxBytesPerSec
			less = rateI < rateJ
		case "disk":
			less = filtered[i].StorageTotalIOPS < filtered[j].StorageTotalIOPS
		default:
			less = filtered[i].CPUUsageCores < filtered[j].CPUUsageCores
		}

		if m.SortReverse {
			return less
		}
		return !less
	})

	return filtered
}

// View renders the interactive table component within the given width and height.
func (m *TableModel) View(width, height int, vms []store.VMSnapshot) string {
	filtered := m.FilterAndSort(vms)

	contentWidth := max(2, width-4)
	contentHeight := max(1, height-2)

	// Adjust cursor bounds
	if m.Cursor >= len(filtered) {
		m.Cursor = max(0, len(filtered)-1)
	}

	headerStr := m.renderHeader(contentWidth)
	headerHeight := lipgloss.Height(headerStr)

	// Visible rows height (accounting for header lines)
	visibleRowsHeight := contentHeight - headerHeight
	if visibleRowsHeight < 1 {
		visibleRowsHeight = 1
	}

	// Adjust scroll offset
	if m.Cursor < m.ScrollOffset {
		m.ScrollOffset = m.Cursor
	} else if m.Cursor >= m.ScrollOffset+visibleRowsHeight {
		m.ScrollOffset = m.Cursor - visibleRowsHeight + 1
	}

	var rows []string
	rows = append(rows, headerStr)

	if len(filtered) == 0 {
		emptyMsg := "No VirtualMachineInstances match filter"
		if m.ScopedNode != "" {
			emptyMsg = fmt.Sprintf("No VMs running on node %s", m.ScopedNode)
		}
		rows = append(rows, padRight(HeaderLabelStyle.Render(emptyMsg), contentWidth))
	} else {
		end := min(len(filtered), m.ScrollOffset+visibleRowsHeight)
		for i := m.ScrollOffset; i < end; i++ {
			vm := filtered[i]
			selected := m.Focused && (i == m.Cursor)
			rows = append(rows, m.renderRow(vm, selected, contentWidth))
		}
	}

	content := strings.Join(rows, "\n")

	style := PanelStyle
	if m.Focused {
		style = PanelFocusedStyle
	}

	return style.Width(width - 2).Height(contentHeight).Render(content)
}

func (m *TableModel) renderHeader(contentWidth int) string {
	sortIndicator := func(col string) string {
		if m.SortColumn != col {
			return " "
		}
		if m.SortReverse {
			return "▲"
		}
		return "▼"
	}

	nsW, nameW, ipW, nodeW, cpuW, memW, rxW, txW, diskW := columnWidths(contentWidth)

	nsLabel := "NAMESPACE"
	if nsW < 9 {
		nsLabel = "NS"
	}
	nameLabel := fmt.Sprintf("%s NAME", sortIndicator("name"))
	cpuLabel := fmt.Sprintf("%s CPU", sortIndicator("cpu"))
	if m.SortColumn == "cpu" && m.SortSaturation {
		cpuLabel = fmt.Sprintf("%s CPU%%", sortIndicator("cpu"))
	}
	memLabel := fmt.Sprintf("%s MEM", sortIndicator("mem"))
	rxLabel := fmt.Sprintf("%s RX", sortIndicator("rx"))
	txLabel := fmt.Sprintf("%s TX", sortIndicator("tx"))
	diskLabel := fmt.Sprintf("%s IOPS", sortIndicator("disk"))
	if diskW < 6 {
		diskLabel = fmt.Sprintf("%s IO", sortIndicator("disk"))
	}

	header := strings.Join([]string{
		padRight(nsLabel, nsW),
		padRight(nameLabel, nameW),
		padRight("IP", ipW),
		padRight("NODE", nodeW),
		padRight(cpuLabel, cpuW),
		padRight(memLabel, memW),
		padRight(rxLabel, rxW),
		padRight(txLabel, txW),
		padRight(diskLabel, diskW),
	}, " ")

	totalW := nsW + 1 + nameW + 1 + ipW + 1 + nodeW + 1 + cpuW + 1 + memW + 1 + rxW + 1 + txW + 1 + diskW
	if contentWidth > totalW {
		header = header + strings.Repeat(" ", contentWidth-totalW)
	}

	return TableHeaderStyle.Width(contentWidth).Render(header)
}

func (m *TableModel) renderRow(vm store.VMSnapshot, selected bool, contentWidth int) string {
	nsW, nameW, ipW, nodeW, cpuW, memW, rxW, txW, diskW := columnWidths(contentWidth)

	var bg lipgloss.TerminalColor
	rowStyle := TableRowStyle
	if selected {
		bg = ColorHighlight
		rowStyle = TableRowSelectedStyle
	}

	sparkWidth := 6
	maxCore := float64(vm.AllottedCPUs)
	if maxCore <= 0 {
		maxCore = 1.0
	}
	sparkline := chart.RenderBrailleSparklineWithBg(vm.CPUHistory, maxCore, sparkWidth, bg)

	cpuVal := fmt.Sprintf("%4.2f/%-2d", vm.CPUUsageCores, vm.AllottedCPUs)
	if m.SortColumn == "cpu" && m.SortSaturation {
		cpuVal = fmt.Sprintf("%5.1f%%", vm.CPUSaturationPercent)
	}

	memStr := "–"
	if vm.HasBalloonStats {
		if memW <= 8 {
			memStr = fmt.Sprintf("%3.0f%%", vm.MemoryUsedPercent)
		} else {
			memStr = fmt.Sprintf("%3.0f%% %s", vm.MemoryUsedPercent, FormatBytes(float64(vm.MemoryUsedBytes)))
		}
	}

	rxStr := "▼" + FormatRate(vm.NetRxBytesPerSec)
	txStr := "▲" + FormatRate(vm.NetTxBytesPerSec)
	iopsStr := fmt.Sprintf("%4.1f", vm.StorageTotalIOPS)

	nameStr := vm.Name
	if vm.IsMigrating {
		nameStr = "⇶ " + vm.Name
	} else if vm.Phase != "" && vm.Phase != "Running" {
		nameStr = "[" + vm.Phase[:min(len(vm.Phase), 4)] + "] " + vm.Name
	}

	ipStr := vm.IP
	if ipStr == "" {
		ipStr = "–"
	}

	nodeStr := vm.Node
	if vm.IsMigrating && vm.MigrationTargetNode != "" {
		nodeStr = fmt.Sprintf("⇶%s", truncate(vm.MigrationTargetNode, 5))
	}

	cNS := truncate(vm.Namespace, nsW)
	cName := truncate(nameStr, nameW)
	cIP := truncate(ipStr, ipW)
	cNode := truncate(nodeStr, nodeW)
	cCpuVal := padRight(cpuVal, max(1, cpuW-(sparkWidth+1)))
	cMem := truncate(memStr, memW)
	cRx := truncate(rxStr, rxW)
	cTx := truncate(txStr, txW)
	cDisk := truncate(iopsStr, diskW)

	totalW := nsW + 1 + nameW + 1 + ipW + 1 + nodeW + 1 + cpuW + 1 + memW + 1 + rxW + 1 + txW + 1 + diskW

	if selected {
		// When selected, explicitly paint ColorHighlight background across every single cell and separator
		line := rowStyle.Render(padRight(cNS, nsW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cName, nameW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cIP, ipW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cNode, nodeW)) +
			rowStyle.Render(" ") +
			sparkline + rowStyle.Render(" "+cCpuVal) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cMem, memW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cRx, rxW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cTx, txW)) +
			rowStyle.Render(" ") +
			rowStyle.Render(padRight(cDisk, diskW))

		if contentWidth > totalW {
			line += rowStyle.Render(strings.Repeat(" ", contentWidth-totalW))
		}
		return line
	}

	// Normal unselected row
	line := strings.Join([]string{
		padRight(cNS, nsW),
		padRight(cName, nameW),
		padRight(cIP, ipW),
		padRight(cNode, nodeW),
		sparkline + " " + cCpuVal,
		padRight(cMem, memW),
		padRight(cRx, rxW),
		padRight(cTx, txW),
		padRight(cDisk, diskW),
	}, " ")

	if contentWidth > totalW {
		line = line + strings.Repeat(" ", contentWidth-totalW)
	}

	return TableRowStyle.Render(line)
}

func padRight(s string, targetWidth int) string {
	w := lipgloss.Width(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

func columnWidths(innerWidth int) (nsW, nameW, ipW, nodeW, cpuW, memW, rxW, txW, diskW int) {
	if innerWidth < 85 {
		nsW = 8
		ipW = 12
		nodeW = 5
		cpuW = 11
		memW = 6
		rxW = 7
		txW = 7
		diskW = 5
	} else if innerWidth < 105 {
		nsW = 9
		ipW = 13
		nodeW = 6
		cpuW = 13
		memW = 8
		rxW = 8
		txW = 8
		diskW = 5
	} else {
		nsW = 10
		ipW = 14
		nodeW = 7
		cpuW = 15
		memW = 10
		rxW = 9
		txW = 9
		diskW = 6
	}

	// 8 single-space column separators
	fixedCols := nsW + ipW + nodeW + cpuW + memW + rxW + txW + diskW + 8
	nameW = innerWidth - fixedCols
	if nameW < 10 {
		nameW = 10
	}

	// If generous horizontal space is available, also widen nsW up to 16 for full "harvester-system"
	if nameW > 28 && nsW < 16 {
		extra := min(nameW-28, 16-nsW)
		nsW += extra
		nameW -= extra
	}

	return
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-2]) + ".."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
