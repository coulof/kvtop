package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coulof/kvtop/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ReplayCollector reads recorded .prom scrape files from a directory.
// It can simulate continuous metric advances across multiple ticks.
type ReplayCollector struct {
	dir       string
	interval  time.Duration
	tick      int
	baseTime  time.Time
	nodeFiles []replayFile
}

type replayFile struct {
	path     string
	nodeName string
}

// NewReplayCollector scans the provided directory for virt-handler-*.prom files.
func NewReplayCollector(dir string, interval time.Duration) (*ReplayCollector, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read replay directory %s: %w", dir, err)
	}

	var files []replayFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "virt-handler-") && strings.HasSuffix(name, ".prom") {
			nodeName := strings.TrimPrefix(name, "virt-handler-")
			nodeName = strings.TrimSuffix(nodeName, ".prom")
			files = append(files, replayFile{
				path:     filepath.Join(dir, name),
				nodeName: nodeName,
			})
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no virt-handler-*.prom files found in %s", dir)
	}

	return &ReplayCollector{
		dir:       dir,
		interval:  interval,
		baseTime:  time.Now(),
		nodeFiles: files,
	}, nil
}

// Name returns the collector identifier.
func (r *ReplayCollector) Name() string {
	return "replay"
}

// Collect reads and parses recorded metric files, advancing timestamps each tick.
func (r *ReplayCollector) Collect(ctx context.Context) ([]VMISample, error) {
	sampleTime := r.baseTime.Add(time.Duration(r.tick) * r.interval)
	var allSamples []VMISample

	for _, rf := range r.nodeFiles {
		f, err := os.Open(rf.path)
		if err != nil {
			return nil, fmt.Errorf("failed to open replay file %s: %w", rf.path, err)
		}

		parsed, err := ParseVirtHandlerMetrics(f, rf.nodeName, sampleTime)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to parse replay file %s: %w", rf.path, err)
		}

		for _, s := range parsed {
			// Simulate realistic counter growth over ticks so rates and graphs populate nicely
			simulated := *s
			if r.tick > 0 {
				simulated.CPUUsageSeconds += float64(r.tick) * (0.15 + 0.05*float64(len(s.Name)%5))
				simulated.NetRxBytesTotal += uint64(r.tick) * uint64(512+128*(len(s.Name)%7))
				simulated.NetTxBytesTotal += uint64(r.tick) * uint64(256+64*(len(s.Name)%3))
				simulated.StorageReadIOPSTotal += uint64(r.tick) * uint64(2+(len(s.Name)%3))
				simulated.StorageWriteIOPSTotal += uint64(r.tick) * uint64(1+(len(s.Name)%2))
			}
			allSamples = append(allSamples, simulated)
		}
	}

	r.tick++
	return allSamples, nil
}

// LoadReplayVMIs loads VMI metadata from vmis.json if present in the replay directory.
func LoadReplayVMIs(dir string) ([]kube.VMIInfo, error) {
	vmiPath := filepath.Join(dir, "vmis.json")
	data, err := os.ReadFile(vmiPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", vmiPath, err)
	}

	var list unstructured.UnstructuredList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse %s as json: %w", vmiPath, err)
	}

	var results []kube.VMIInfo
	for _, item := range list.Items {
		itemCopy := item
		results = append(results, kube.ExtractVMIInfo(&itemCopy))
	}

	return results, nil
}

// LoadReplayVirshStats reads virsh-domstats.txt from the replay directory if available.
func LoadReplayVirshStats(dir string) (VirshStats, error) {
	path := filepath.Join(dir, "virsh-domstats.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultMockVirshStats(), nil
		}
		return VirshStats{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return ParseVirshBlock(string(data), time.Now()), nil
}

func defaultMockVirshStats() VirshStats {
	return VirshStats{
		Timestamp:             time.Now(),
		DomainName:            "replay-vm",
		BalloonCurrentBytes:   8 * 1024 * 1024 * 1024,
		BalloonMaxBytes:       8 * 1024 * 1024 * 1024,
		BalloonUsableBytes:    6 * 1024 * 1024 * 1024,
		BalloonAvailableBytes: 7 * 1024 * 1024 * 1024,
		BalloonUnusedBytes:    5 * 1024 * 1024 * 1024,
		BalloonRSSBytes:       950 * 1024 * 1024,
		VCPUs: []VCPUStat{
			{ID: 0, State: 1, TimeNs: 12000000000, WaitNs: 500000, DelayNs: 1200000},
			{ID: 1, State: 1, TimeNs: 18000000000, WaitNs: 400000, DelayNs: 900000},
		},
		NICs: []NICStat{
			{Name: "default", RxBytes: 150000000, RxPackets: 250000, TxBytes: 45000000, TxPackets: 95000},
		},
		Disks: []DiskStat{
			{Name: "rootdisk", Path: "/dev/disk-0", CapacityBytes: 40 * 1024 * 1024 * 1024, ReadReqs: 15000, ReadBytes: 250000000, WriteReqs: 85000, WriteBytes: 850000000},
		},
	}
}
