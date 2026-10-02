package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui/chart"
)

// DetailModel manages the VM deep-dive view with per-vCPU, per-disk, per-NIC, and launcher RSS stats.
type DetailModel struct {
	VM          store.VMSnapshot
	LauncherPod string
	Stats       collect.VirshStats
	HasStats    bool
	ScrollY     int

	prevStats      collect.VirshStats
	hasPrev        bool
	vcpuHistory    map[int]*store.RingBuffer
	vcpuRates      map[int]float64
	nicRxRates     map[string]float64
	nicTxRates     map[string]float64
	diskReadRates  map[string]float64
	diskWriteRates map[string]float64
	diskReadIOPS   map[string]float64
	diskWriteIOPS  map[string]float64
}

// NewDetailModel initializes a detail view for the selected VM.
func NewDetailModel(vm store.VMSnapshot, podName string) *DetailModel {
	return &DetailModel{
		VM:             vm,
		LauncherPod:    podName,
		vcpuHistory:    make(map[int]*store.RingBuffer),
		vcpuRates:      make(map[int]float64),
		nicRxRates:     make(map[string]float64),
		nicTxRates:     make(map[string]float64),
		diskReadRates:  make(map[string]float64),
		diskWriteRates: make(map[string]float64),
		diskReadIOPS:   make(map[string]float64),
		diskWriteIOPS:  make(map[string]float64),
	}
}

// UpdateStats ingests a fresh snapshot from the virsh domstats stream and computes live rates.
func (d *DetailModel) UpdateStats(s collect.VirshStats) {
	if d.hasPrev && s.Timestamp.After(d.prevStats.Timestamp) {
		dt := s.Timestamp.Sub(d.prevStats.Timestamp).Seconds()
		if dt > 0 {
			// Per-vCPU utilization rate delta
			prevVCPUMap := make(map[int]collect.VCPUStat)
			for _, pv := range d.prevStats.VCPUs {
				prevVCPUMap[pv.ID] = pv
			}

			for _, v := range s.VCPUs {
				rb, ok := d.vcpuHistory[v.ID]
				if !ok {
					rb = store.NewRingBuffer(30)
					d.vcpuHistory[v.ID] = rb
				}

				if pv, found := prevVCPUMap[v.ID]; found && v.TimeNs >= pv.TimeNs {
					dTimeSec := float64(v.TimeNs-pv.TimeNs) / 1e9
					utilPct := (dTimeSec / dt) * 100.0
					if utilPct > 100.0 {
						utilPct = 100.0
					}
					d.vcpuRates[v.ID] = utilPct
					rb.Push(s.Timestamp, utilPct)
				}
			}

			// Per-NIC live rates
			prevNICMap := make(map[string]collect.NICStat)
			for _, pn := range d.prevStats.NICs {
				prevNICMap[pn.Name] = pn
			}
			for _, nic := range s.NICs {
				if pn, found := prevNICMap[nic.Name]; found {
					if nic.RxBytes >= pn.RxBytes {
						d.nicRxRates[nic.Name] = float64(nic.RxBytes-pn.RxBytes) / dt
					}
					if nic.TxBytes >= pn.TxBytes {
						d.nicTxRates[nic.Name] = float64(nic.TxBytes-pn.TxBytes) / dt
					}
				}
			}

			// Per-Disk live rates
			prevDiskMap := make(map[string]collect.DiskStat)
			for _, pd := range d.prevStats.Disks {
				prevDiskMap[pd.Name] = pd
			}
			for _, disk := range s.Disks {
				if pd, found := prevDiskMap[disk.Name]; found {
					if disk.ReadBytes >= pd.ReadBytes {
						d.diskReadRates[disk.Name] = float64(disk.ReadBytes-pd.ReadBytes) / dt
					}
					if disk.WriteBytes >= pd.WriteBytes {
						d.diskWriteRates[disk.Name] = float64(disk.WriteBytes-pd.WriteBytes) / dt
					}
					if disk.ReadReqs >= pd.ReadReqs {
						d.diskReadIOPS[disk.Name] = float64(disk.ReadReqs-pd.ReadReqs) / dt
					}
					if disk.WriteReqs >= pd.WriteReqs {
						d.diskWriteIOPS[disk.Name] = float64(disk.WriteReqs-pd.WriteReqs) / dt
					}
				}
			}
		}
	}

	d.prevStats = s
	d.hasPrev = true
	d.Stats = s
	d.HasStats = true
}

func (d *DetailModel) ScrollUp() {
	if d.ScrollY > 0 {
		d.ScrollY--
	}
}

func (d *DetailModel) ScrollDown() {
	d.ScrollY++
}

// View renders the detail page.
func (d *DetailModel) View(width, height int) string {
	innerWidth := max(2, width-4)
	innerHeight := max(1, height-2)
	textWidth := max(2, width-6)

	var sections []string

	// Header Banner
	line1 := TitleStyle.Render(fmt.Sprintf("VM: %s/%s  │  Node: %s  │  Phase: %s",
		d.VM.Namespace,
		d.VM.Name,
		d.VM.Node,
		d.VM.Phase,
	))
	line2 := HeaderLabelStyle.Render(fmt.Sprintf("Pod: %s  │  Press [Esc] or [q] to return to Main Page", d.LauncherPod))
	banner := line1 + "\n" + line2 + "\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", textWidth))
	sections = append(sections, banner)

	// Memory & Launcher RSS Section
	sections = append(sections, d.renderMemorySection(innerWidth))

	// Per-vCPU Breakdown (with sparklines and live rates)
	sections = append(sections, d.renderVCPUSection(innerWidth))

	// Per-NIC Breakdown (with live throughput rates)
	sections = append(sections, d.renderNICSection(innerWidth))

	// Per-Disk Breakdown (with live IOPS and traffic)
	sections = append(sections, d.renderDiskSection(innerWidth))

	fullContent := strings.Join(sections, "\n\n")

	// Handle vertical viewport scrolling
	lines := strings.Split(fullContent, "\n")
	if d.ScrollY >= len(lines) {
		d.ScrollY = max(0, len(lines)-1)
	}

	visibleEnd := min(len(lines), d.ScrollY+innerHeight)
	visibleLines := lines[d.ScrollY:visibleEnd]
	content := strings.Join(visibleLines, "\n")

	return PanelFocusedStyle.Width(width - 2).Height(innerHeight).Render(content)
}

func (d *DetailModel) renderMemorySection(width int) string {
	title := HeaderValueStyle.Render("MEMORY & HOST FOOTPRINT")
	rss := "–"
	usable := "–"
	unused := "–"
	alloc := "–"
	avail := "–"
	guestUsed := "–"

	if d.HasStats {
		rss = FormatBytes(float64(d.Stats.BalloonRSSBytes))
		usable = FormatBytes(float64(d.Stats.BalloonUsableBytes))
		unused = FormatBytes(float64(d.Stats.BalloonUnusedBytes))
		alloc = FormatBytes(float64(d.Stats.BalloonCurrentBytes))
		avail = FormatBytes(float64(d.Stats.BalloonAvailableBytes))
		if d.Stats.BalloonCurrentBytes > d.Stats.BalloonUsableBytes {
			guestUsed = FormatBytes(float64(d.Stats.BalloonCurrentBytes - d.Stats.BalloonUsableBytes))
		}
	} else if d.VM.HasBalloonStats {
		rss = FormatBytes(float64(d.VM.MemoryUsedBytes))
		alloc = FormatBytes(float64(d.VM.MemoryTotalBytes))
	}

	coloredRss := lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render(rss)

	row1 := padRight("Launcher RSS (Host Cost):", 26) + padRight(coloredRss, 12) + "  " +
		padRight("Guest Usable:", 16) + padRight(HeaderValueStyle.Render(usable), 10)
	row2 := padRight("Balloon Allocated:", 26) + padRight(HeaderValueStyle.Render(alloc), 12) + "  " +
		padRight("Guest Unused:", 16) + padRight(HeaderValueStyle.Render(unused), 10)
	row3 := padRight("Guest Available:", 26) + padRight(HeaderValueStyle.Render(avail), 12) + "  " +
		padRight("Guest Used:", 16) + padRight(HeaderValueStyle.Render(guestUsed), 10)

	grid := strings.Join([]string{row1, row2, row3}, "\n")
	return title + "\n" + grid
}

func (d *DetailModel) renderVCPUSection(width int) string {
	title := HeaderValueStyle.Render("PER-vCPU BREAKDOWN")
	header := strings.Join([]string{
		padRight("vCPU", 8),
		padRight("STATE", 10),
		padRight("SPARK+UTIL%", 16),
		padRight("CPU TIME", 14),
		padRight("WAIT TIME", 12),
		padRight("QUEUE DELAY", 14),
	}, " ")

	var rows []string
	rows = append(rows, TableHeaderStyle.Render(header))

	if !d.HasStats || len(d.Stats.VCPUs) == 0 {
		rows = append(rows, HeaderLabelStyle.Render("  Streaming vCPU statistics from virt-launcher compute container..."))
	} else {
		for _, v := range d.Stats.VCPUs {
			stateStr := "Running"
			stateStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
			if v.State != 1 {
				stateStr = "Blocked"
				stateStyle = lipgloss.NewStyle().Foreground(ColorWarning)
			}

			timeSec := float64(v.TimeNs) / 1e9
			waitMs := float64(v.WaitNs) / 1e6
			delayMs := float64(v.DelayNs) / 1e6

			utilPct := d.vcpuRates[v.ID]
			var history []float64
			if rb, ok := d.vcpuHistory[v.ID]; ok {
				history = rb.Values()
			}
			sparkline := chart.RenderBrailleSparkline(history, 100.0, 6)
			utilStr := fmt.Sprintf("%s %5.1f%%", sparkline, utilPct)

			row := strings.Join([]string{
				padRight(fmt.Sprintf("cpu #%d", v.ID), 8),
				padRight(stateStyle.Render(stateStr), 10),
				padRight(utilStr, 16),
				padRight(fmt.Sprintf("%.2fs", timeSec), 14),
				padRight(fmt.Sprintf("%.1fms", waitMs), 12),
				padRight(fmt.Sprintf("%.1fms", delayMs), 14),
			}, " ")
			rows = append(rows, row)
		}
	}

	return title + "\n" + strings.Join(rows, "\n")
}

func (d *DetailModel) renderNICSection(width int) string {
	title := HeaderValueStyle.Render("PER-NIC NETWORK INTERFACES")
	header := strings.Join([]string{
		padRight("INTERFACE", 12),
		padRight("LIVE RX/s", 10),
		padRight("TOTAL RX", 10),
		padRight("RX PKTS/ERR", 16),
		padRight("LIVE TX/s", 10),
		padRight("TOTAL TX", 10),
		padRight("TX PKTS/ERR", 16),
	}, " ")
	sep := lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", lipgloss.Width(header)))

	var rows []string
	rows = append(rows, lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render(header))
	rows = append(rows, sep)

	if !d.HasStats || len(d.Stats.NICs) == 0 {
		rows = append(rows, HeaderLabelStyle.Render("  Streaming interface statistics..."))
	} else {
		for _, nic := range d.Stats.NICs {
			name := nic.Name
			if name == "" {
				name = "vnic"
			}
			rxRate := FormatRate(d.nicRxRates[nic.Name])
			txRate := FormatRate(d.nicTxRates[nic.Name])
			rxStr := FormatBytes(float64(nic.RxBytes))
			txStr := FormatBytes(float64(nic.TxBytes))
			rxPkts := fmt.Sprintf("%d (err:%d)", nic.RxPackets, nic.RxErrors)
			txPkts := fmt.Sprintf("%d (err:%d)", nic.TxPackets, nic.TxErrors)

			row := strings.Join([]string{
				padRight(truncate(name, 12), 12),
				padRight(rxRate, 10),
				padRight(rxStr, 10),
				padRight(rxPkts, 16),
				padRight(txRate, 10),
				padRight(txStr, 10),
				padRight(txPkts, 16),
			}, " ")
			rows = append(rows, row)
		}
	}

	return title + "\n" + strings.Join(rows, "\n")
}

func (d *DetailModel) renderDiskSection(width int) string {
	title := HeaderValueStyle.Render("PER-DISK BLOCK DEVICES")
	header := strings.Join([]string{
		padRight("DEVICE", 8),
		padRight("CAPACITY", 9),
		padRight("RD RATE", 14),
		padRight("TOTAL READ", 16),
		padRight("WR RATE", 14),
		padRight("TOTAL WRITE", 16),
		padRight("FLUSH", 6),
	}, " ")
	sep := lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Repeat("─", lipgloss.Width(header)))

	var rows []string
	rows = append(rows, lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render(header))
	rows = append(rows, sep)

	if !d.HasStats || len(d.Stats.Disks) == 0 {
		rows = append(rows, HeaderLabelStyle.Render("  Streaming block device statistics..."))
	} else {
		for _, disk := range d.Stats.Disks {
			name := disk.Name
			if name == "" {
				name = "disk"
			}
			capStr := FormatBytes(float64(disk.CapacityBytes))
			rdRateStr := fmt.Sprintf("%s (%.0f io)", FormatRate(d.diskReadRates[disk.Name]), d.diskReadIOPS[disk.Name])
			wrRateStr := fmt.Sprintf("%s (%.0f io)", FormatRate(d.diskWriteRates[disk.Name]), d.diskWriteIOPS[disk.Name])
			rdStr := fmt.Sprintf("%s (%d)", FormatBytes(float64(disk.ReadBytes)), disk.ReadReqs)
			wrStr := fmt.Sprintf("%s (%d)", FormatBytes(float64(disk.WriteBytes)), disk.WriteReqs)
			flStr := fmt.Sprintf("%d", disk.FlushReqs)

			row := strings.Join([]string{
				padRight(truncate(name, 8), 8),
				padRight(capStr, 9),
				padRight(rdRateStr, 14),
				padRight(rdStr, 16),
				padRight(wrRateStr, 14),
				padRight(wrStr, 16),
				padRight(flStr, 6),
			}, " ")
			rows = append(rows, row)
		}
	}

	return title + "\n" + strings.Join(rows, "\n")
}
