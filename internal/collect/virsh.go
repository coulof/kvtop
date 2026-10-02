package collect

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// VirshStats contains parsed domain statistics from `virsh domstats`.
type VirshStats struct {
	Timestamp  time.Time
	DomainName string

	// CPU
	CPUTimeNs   uint64
	CPUUserNs   uint64
	CPUSystemNs uint64

	// Per-vCPU breakdown
	VCPUs []VCPUStat

	// Balloon / Memory
	BalloonCurrentBytes   uint64
	BalloonMaxBytes       uint64
	BalloonUnusedBytes    uint64
	BalloonAvailableBytes uint64
	BalloonUsableBytes    uint64
	BalloonRSSBytes       uint64 // Launcher RSS (host memory footprint)

	// Per-NIC breakdown
	NICs []NICStat

	// Per-Disk breakdown
	Disks []DiskStat
}

// VCPUStat contains per-core hypervisor timing and delay metrics.
type VCPUStat struct {
	ID      int
	State   int
	TimeNs  uint64
	WaitNs  uint64
	DelayNs uint64
}

// NICStat contains per-vNIC network counters.
type NICStat struct {
	Name      string
	RxBytes   uint64
	RxPackets uint64
	RxErrors  uint64
	RxDrop    uint64
	TxBytes   uint64
	TxPackets uint64
	TxErrors  uint64
	TxDrop    uint64
}

// DiskStat contains per-block device IOPS and traffic counters.
type DiskStat struct {
	Name            string
	Path            string
	CapacityBytes   uint64
	AllocationBytes uint64
	PhysicalBytes   uint64
	ReadReqs        uint64
	ReadBytes       uint64
	ReadTimeNs      uint64
	WriteReqs       uint64
	WriteBytes      uint64
	WriteTimeNs     uint64
	FlushReqs       uint64
	FlushTimeNs     uint64
}

// ParseVirshBlock parses a single snapshot of `virsh domstats` output.
func ParseVirshBlock(block string, t time.Time) VirshStats {
	stats := VirshStats{
		Timestamp: t,
	}

	lines := strings.Split(block, "\n")
	vcpuMap := make(map[int]*VCPUStat)
	diskMap := make(map[int]*DiskStat)
	nicMap := make(map[int]*NICStat)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "Domain: '") {
			parts := strings.Split(line, "'")
			if len(parts) >= 2 {
				stats.DomainName = parts[1]
			}
			continue
		}

		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		switch {
		case key == "cpu.time":
			stats.CPUTimeNs, _ = strconv.ParseUint(val, 10, 64)
		case key == "cpu.user":
			stats.CPUUserNs, _ = strconv.ParseUint(val, 10, 64)
		case key == "cpu.system":
			stats.CPUSystemNs, _ = strconv.ParseUint(val, 10, 64)

		case key == "balloon.current":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonCurrentBytes = kib * 1024
		case key == "balloon.maximum":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonMaxBytes = kib * 1024
		case key == "balloon.unused":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonUnusedBytes = kib * 1024
		case key == "balloon.available":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonAvailableBytes = kib * 1024
		case key == "balloon.usable":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonUsableBytes = kib * 1024
		case key == "balloon.rss":
			kib, _ := strconv.ParseUint(val, 10, 64)
			stats.BalloonRSSBytes = kib * 1024

		case strings.HasPrefix(key, "vcpu."):
			parseVCPUKey(key, val, vcpuMap)

		case strings.HasPrefix(key, "net."):
			parseNICKey(key, val, nicMap)

		case strings.HasPrefix(key, "block."):
			parseDiskKey(key, val, diskMap)
		}
	}

	// Assemble VCPUs
	for i := 0; i < len(vcpuMap); i++ {
		if v, ok := vcpuMap[i]; ok {
			stats.VCPUs = append(stats.VCPUs, *v)
		}
	}

	// Assemble NICs
	for i := 0; i < len(nicMap); i++ {
		if n, ok := nicMap[i]; ok {
			stats.NICs = append(stats.NICs, *n)
		}
	}

	// Assemble Disks
	for i := 0; i < len(diskMap); i++ {
		if d, ok := diskMap[i]; ok {
			stats.Disks = append(stats.Disks, *d)
		}
	}

	return stats
}

func parseVCPUKey(key, val string, vcpuMap map[int]*VCPUStat) {
	// Format: vcpu.<id>.<metric>
	parts := strings.Split(key, ".")
	if len(parts) < 3 {
		return
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	v, ok := vcpuMap[id]
	if !ok {
		v = &VCPUStat{ID: id}
		vcpuMap[id] = v
	}

	metric := strings.Join(parts[2:], ".")
	switch metric {
	case "state":
		v.State, _ = strconv.Atoi(val)
	case "time":
		v.TimeNs, _ = strconv.ParseUint(val, 10, 64)
	case "wait":
		v.WaitNs, _ = strconv.ParseUint(val, 10, 64)
	case "delay":
		v.DelayNs, _ = strconv.ParseUint(val, 10, 64)
	}
}

func parseNICKey(key, val string, nicMap map[int]*NICStat) {
	// Format: net.<id>.<metric>
	parts := strings.Split(key, ".")
	if len(parts) < 3 {
		return
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	n, ok := nicMap[id]
	if !ok {
		n = &NICStat{}
		nicMap[id] = n
	}

	metric := strings.Join(parts[2:], ".")
	switch metric {
	case "name":
		n.Name = val
	case "rx.bytes":
		n.RxBytes, _ = strconv.ParseUint(val, 10, 64)
	case "rx.pkts":
		n.RxPackets, _ = strconv.ParseUint(val, 10, 64)
	case "rx.errs":
		n.RxErrors, _ = strconv.ParseUint(val, 10, 64)
	case "rx.drop":
		n.RxDrop, _ = strconv.ParseUint(val, 10, 64)
	case "tx.bytes":
		n.TxBytes, _ = strconv.ParseUint(val, 10, 64)
	case "tx.pkts":
		n.TxPackets, _ = strconv.ParseUint(val, 10, 64)
	case "tx.errs":
		n.TxErrors, _ = strconv.ParseUint(val, 10, 64)
	case "tx.drop":
		n.TxDrop, _ = strconv.ParseUint(val, 10, 64)
	}
}

func parseDiskKey(key, val string, diskMap map[int]*DiskStat) {
	// Format: block.<id>.<metric>
	parts := strings.Split(key, ".")
	if len(parts) < 3 {
		return
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}
	d, ok := diskMap[id]
	if !ok {
		d = &DiskStat{}
		diskMap[id] = d
	}

	metric := strings.Join(parts[2:], ".")
	switch metric {
	case "name":
		d.Name = val
	case "path":
		d.Path = val
	case "capacity":
		d.CapacityBytes, _ = strconv.ParseUint(val, 10, 64)
	case "allocation":
		d.AllocationBytes, _ = strconv.ParseUint(val, 10, 64)
	case "physical":
		d.PhysicalBytes, _ = strconv.ParseUint(val, 10, 64)
	case "rd.reqs":
		d.ReadReqs, _ = strconv.ParseUint(val, 10, 64)
	case "rd.bytes":
		d.ReadBytes, _ = strconv.ParseUint(val, 10, 64)
	case "rd.times":
		d.ReadTimeNs, _ = strconv.ParseUint(val, 10, 64)
	case "wr.reqs":
		d.WriteReqs, _ = strconv.ParseUint(val, 10, 64)
	case "wr.bytes":
		d.WriteBytes, _ = strconv.ParseUint(val, 10, 64)
	case "wr.times":
		d.WriteTimeNs, _ = strconv.ParseUint(val, 10, 64)
	case "fl.reqs":
		d.FlushReqs, _ = strconv.ParseUint(val, 10, 64)
	case "fl.times":
		d.FlushTimeNs, _ = strconv.ParseUint(val, 10, 64)
	}
}

// StreamVirshDomstats reads from a continuous virsh domstats output stream and delivers snapshots to out channel.
func StreamVirshDomstats(r io.Reader, out chan<- VirshStats, stopCh <-chan struct{}) {
	scanner := bufio.NewScanner(r)
	var currentBlock strings.Builder

	for scanner.Scan() {
		select {
		case <-stopCh:
			return
		default:
		}

		line := scanner.Text()
		if strings.HasPrefix(line, "Domain: '") && currentBlock.Len() > 0 {
			stats := ParseVirshBlock(currentBlock.String(), time.Now())
			select {
			case <-stopCh:
				return
			case out <- stats:
			}
			currentBlock.Reset()
		}
		currentBlock.WriteString(line + "\n")
	}

	if currentBlock.Len() > 0 {
		stats := ParseVirshBlock(currentBlock.String(), time.Now())
		select {
		case <-stopCh:
			return
		case out <- stats:
		default:
		}
	}
}
