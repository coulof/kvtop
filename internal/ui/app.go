package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
)

type FocusArea int

const (
	FocusTable FocusArea = iota
	FocusNodes
)

// TickMsg signals that a metrics refresh cycle should occur.
type TickMsg time.Time

// VirshStatsMsg delivers streaming detail stats into Bubbletea.
type VirshStatsMsg collect.VirshStats

// DetailStreamStarter initiates a virsh domstats exec stream for the selected VM.
type DetailStreamStarter func(ctx context.Context, namespace, vmiName string, statsChan chan<- collect.VirshStats) (launcherPod string, err error)

// AppModel is the root Bubbletea model for kvtop.
type AppModel struct {
	queryEngine     query.SnapshotProvider
	interval        time.Duration
	width           int
	height          int
	ready           bool
	focus           FocusArea
	isFiltering     bool
	filterText      string
	namespaceFilter string

	header HeaderModel
	nodes  NodesModel
	table  *TableModel
	footer FooterModel
	detail *DetailModel
	help   *HelpModel

	showHelp bool

	detailStreamStarter DetailStreamStarter
	detailCancel        context.CancelFunc
	statsChan           chan collect.VirshStats

	lastSnapshot store.StoreSnapshot
}

// NewAppModel creates the top-level application model reading through query.SnapshotProvider.
func NewAppModel(engine query.SnapshotProvider, clusterName, kubeVersion string, interval time.Duration) *AppModel {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &AppModel{
		queryEngine: engine,
		interval:    interval,
		focus:       FocusTable,
		header: HeaderModel{
			ClusterName: clusterName,
			KubeVersion: kubeVersion,
			Interval:    interval,
		},
		nodes: NodesModel{
			Focused: false,
		},
		table: NewTableModel(),
		footer: FooterModel{
			SortColumn: "name",
		},
		help: NewHelpModel(),
	}
}

// SetDetailStreamStarter configures the streaming callback for virsh domstats.
func (m *AppModel) SetDetailStreamStarter(starter DetailStreamStarter) {
	m.detailStreamStarter = starter
}

func (m AppModel) Init() tea.Cmd {
	return m.tickCmd()
}

func (m AppModel) tickCmd() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Background store refresh tick always updates the snapshot and reschedules
	if _, isTick := msg.(TickMsg); isTick {
		m.lastSnapshot = m.queryEngine.Snapshot(m.namespaceFilter)
		return m, m.tickCmd()
	}

	// If Help modal is active, route keys to help model
	if m.showHelp {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
				return m, nil
			case "up", "k":
				m.help.ScrollUp()
				return m, nil
			case "down", "j":
				m.help.ScrollDown()
				return m, nil
			}
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		}
		return m, nil
	}

	// If Detail View is active, route keys and streaming stats to detail model
	if m.detail != nil {
		switch msg := msg.(type) {
		case VirshStatsMsg:
			m.detail.UpdateStats(collect.VirshStats(msg))
			if m.statsChan != nil {
				return m, listenForVirshStatsCmd(m.statsChan)
			}
			return m, nil
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				if m.detailCancel != nil {
					m.detailCancel()
					m.detailCancel = nil
				}
				m.detail = nil
				m.statsChan = nil
				return m, nil
			case "up", "k":
				m.detail.ScrollUp()
				return m, nil
			case "down", "j":
				m.detail.ScrollDown()
				return m, nil
			}
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		// Search filter input mode
		if m.isFiltering {
			switch msg.Type {
			case tea.KeyEnter:
				m.isFiltering = false
				m.table.FilterQuery = m.filterText
				m.footer.IsFiltering = false
				m.footer.FilterQuery = m.filterText
				return m, nil
			case tea.KeyEsc:
				m.isFiltering = false
				m.filterText = ""
				m.table.FilterQuery = ""
				m.footer.IsFiltering = false
				m.footer.FilterQuery = ""
				return m, nil
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
					m.footer.FilterQuery = m.filterText
				}
				return m, nil
			case tea.KeyRunes:
				m.filterText += string(msg.Runes)
				m.footer.FilterQuery = m.filterText
				return m, nil
			default:
				return m, nil
			}
		}

		// Normal navigation and command mode
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			if m.focus == FocusTable {
				m.focus = FocusNodes
				m.nodes.Focused = true
				m.table.Focused = false
			} else {
				m.focus = FocusTable
				m.nodes.Focused = false
				m.table.Focused = true
			}
			return m, nil

		case "/":
			m.isFiltering = true
			m.footer.IsFiltering = true
			m.filterText = ""
			m.footer.FilterQuery = ""
			return m, nil

		case "o":
			m.table.SortColumn = "name"
			m.footer.SortColumn = "name"
			return m, nil

		case "c":
			m.table.SortColumn = "cpu"
			m.footer.SortColumn = "cpu"
			return m, nil
		case "m":
			m.table.SortColumn = "mem"
			m.footer.SortColumn = "mem"
			return m, nil
		case "n":
			m.table.SortColumn = "net"
			m.footer.SortColumn = "net"
			return m, nil
		case "d":
			m.table.SortColumn = "disk"
			m.footer.SortColumn = "disk"
			return m, nil
		case "s":
			m.table.ToggleCPUSort()
			m.footer.SortSat = m.table.SortSaturation
			return m, nil
		case "r":
			m.table.ToggleReverse()
			m.footer.SortReverse = m.table.SortReverse
			return m, nil
		case "left":
			m.table.PrevSortColumn()
			m.footer.SortColumn = m.table.SortColumn
			return m, nil
		case "right":
			m.table.NextSortColumn()
			m.footer.SortColumn = m.table.SortColumn
			return m, nil

		case "+", "=":
			if m.interval > 500*time.Millisecond {
				m.interval -= 500 * time.Millisecond
				m.header.Interval = m.interval
			}
			return m, nil
		case "-", "_":
			if m.interval < 10*time.Second {
				m.interval += 500 * time.Millisecond
				m.header.Interval = m.interval
			}
			return m, nil

		case "?":
			m.showHelp = true
			return m, nil

		case "up", "k":
			if m.focus == FocusTable {
				m.table.MoveUp()
			} else {
				m.nodes.MoveUp()
			}
			return m, nil

		case "down", "j":
			if m.focus == FocusTable {
				filtered := m.table.FilterAndSort(m.lastSnapshot.VMs)
				m.table.MoveDown(len(filtered))
			} else {
				m.nodes.MoveDown(len(m.lastSnapshot.Nodes))
			}
			return m, nil

		case " ":
			if m.focus == FocusNodes {
				m.nodes.ToggleScope(m.lastSnapshot.Nodes)
				m.table.ScopedNode = m.nodes.ScopedNode
			}
			return m, nil

		case "enter":
			if m.focus == FocusNodes {
				m.nodes.ToggleScope(m.lastSnapshot.Nodes)
				m.table.ScopedNode = m.nodes.ScopedNode
				return m, nil
			}

			// Open Detail View for selected VM
			filtered := m.table.FilterAndSort(m.lastSnapshot.VMs)
			if len(filtered) == 0 || m.table.Cursor >= len(filtered) {
				return m, nil
			}
			selectedVM := filtered[m.table.Cursor]

			ctx, cancel := context.WithCancel(context.Background())
			m.detailCancel = cancel
			statsChan := make(chan collect.VirshStats, 10)
			m.statsChan = statsChan

			podName := "virt-launcher-" + selectedVM.Name
			if m.detailStreamStarter != nil {
				if pod, err := m.detailStreamStarter(ctx, selectedVM.Namespace, selectedVM.Name, statsChan); err == nil && pod != "" {
					podName = pod
				}
			} else if m.queryEngine != nil {
				if pod, err := m.queryEngine.StreamVirshStats(ctx, selectedVM.Namespace, selectedVM.Name, statsChan); err == nil && pod != "" {
					podName = pod
				}
			}

			m.detail = NewDetailModel(selectedVM, podName)
			return m, listenForVirshStatsCmd(statsChan)

		case "home", "g":
			if m.focus == FocusTable {
				m.table.MoveTop()
			}
			return m, nil

		case "end", "G":
			if m.focus == FocusTable {
				filtered := m.table.FilterAndSort(m.lastSnapshot.VMs)
				m.table.MoveBottom(len(filtered))
			}
			return m, nil
		}
	}

	return m, nil
}

func (m AppModel) View() string {
	if !m.ready {
		return "Initializing kvtop..."
	}

	if m.showHelp {
		return m.help.View(m.width, m.height)
	}

	if m.detail != nil {
		return m.detail.View(m.width, m.height)
	}

	snap := m.lastSnapshot
	if len(snap.Nodes) == 0 && len(snap.VMs) == 0 {
		snap = m.queryEngine.Snapshot(m.namespaceFilter)
	}

	headerView := m.header.View(m.width, snap, len(snap.Nodes))
	footerView := m.footer.View(m.width)

	headerHeight := lipgloss.Height(headerView)
	footerHeight := lipgloss.Height(footerView)
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 10 {
		availableHeight = 10
	}

	var bodyView string

	// Responsive breakpoint at 100 columns as specified in AGENTS.md
	if m.width < 100 {
		// Narrow layout: Stacked Nodes panel on top, VM table below
		nodesHeight := len(snap.Nodes) + 5
		if nodesHeight > availableHeight/2 {
			nodesHeight = availableHeight / 2
		}
		if nodesHeight < 7 {
			nodesHeight = 7
		}
		tableHeight := availableHeight - nodesHeight
		if tableHeight < 5 {
			tableHeight = 5
		}

		nodesView := m.nodes.View(m.width, nodesHeight, snap.Nodes)
		tableView := m.table.View(m.width, tableHeight, snap.VMs)
		bodyView = lipgloss.JoinVertical(lipgloss.Left, nodesView, tableView)
	} else {
		// Wide layout:
		// Top row: CPU history (left 45%) + Nodes table (right 55%)
		// Outer height is sized to show all hosts (1 title + 2 header + N hosts + 2 borders)
		topHeight := len(snap.Nodes) + 5
		if topHeight < 7 {
			topHeight = 7
		}
		if topHeight > availableHeight/2 {
			topHeight = availableHeight / 2
		}

		bottomHeight := availableHeight - topHeight
		if bottomHeight < 8 {
			bottomHeight = 8
		}

		leftTopWidth := (m.width * 45) / 100
		rightTopWidth := m.width - leftTopWidth

		leftBottomWidth := (m.width * 30) / 100
		rightBottomWidth := m.width - leftBottomWidth

		cpuView := RenderCPUPanel(leftTopWidth, topHeight, snap)
		nodesView := m.nodes.View(rightTopWidth, topHeight, snap.Nodes)
		topRow := lipgloss.JoinHorizontal(lipgloss.Top, cpuView, nodesView)

		// Bottom row: Mem panel (top half) + Net panel (bottom half) on left, VM Table on right
		memHeight := bottomHeight / 2
		netHeight := bottomHeight - memHeight

		memView := RenderMemPanel(leftBottomWidth, memHeight, snap)
		netView := RenderNetPanel(leftBottomWidth, netHeight, snap)
		leftBottomView := lipgloss.JoinVertical(lipgloss.Left, memView, netView)

		tableView := m.table.View(rightBottomWidth, bottomHeight, snap.VMs)
		bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, leftBottomView, tableView)

		bodyView = lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow)
	}

	bodyView = lipgloss.NewStyle().MaxHeight(availableHeight).Render(bodyView)
	return lipgloss.JoinVertical(lipgloss.Left, headerView, bodyView, footerView)
}

func footerViewHeight(h int) int {
	if h < 1 {
		return 1
	}
	return h
}

func listenForVirshStatsCmd(ch <-chan collect.VirshStats) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		s, ok := <-ch
		if !ok {
			return nil
		}
		return VirshStatsMsg(s)
	}
}
