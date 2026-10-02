package collect_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
)

func TestParseVirtHandlerMetrics_Fixtures(t *testing.T) {
	testCases := []struct {
		name                 string
		fixtureFile          string
		nodeFallback         string
		expectedVMKey        string
		expectBalloon        bool
		minCPUUsageSeconds   float64
		minMemoryDomainBytes uint64
		minNetRxBytes        uint64
		minStorageIOPS       uint64
	}{
		{
			name:                 "hv-01 spike-it (with balloon stats)",
			fixtureFile:          "../../testdata/virt-handler-hv-01.prom",
			nodeFallback:         "hv-01",
			expectedVMKey:        "default/spike-it",
			expectBalloon:        true,
			minCPUUsageSeconds:   1000.0,
			minMemoryDomainBytes: 1024 * 1024 * 1024,
			minNetRxBytes:        100000,
			minStorageIOPS:       100,
		},
		{
			name:                 "hv-02 ubuntu-ridge-wyi (with balloon stats)",
			fixtureFile:          "../../testdata/virt-handler-hv-02.prom",
			nodeFallback:         "hv-02",
			expectedVMKey:        "default/ubuntu-ridge-wyi",
			expectBalloon:        true,
			minCPUUsageSeconds:   50.0,
			minMemoryDomainBytes: 1024 * 1024 * 1024,
			minNetRxBytes:        100000,
			minStorageIOPS:       10,
		},
		{
			name:                 "hv-03 tumbleweed-flint-florian (with balloon stats)",
			fixtureFile:          "../../testdata/virt-handler-hv-03.prom",
			nodeFallback:         "hv-03",
			expectedVMKey:        "default/tumbleweed-flint-florian",
			expectBalloon:        true,
			minCPUUsageSeconds:   50.0,
			minMemoryDomainBytes: 1024 * 1024 * 1024,
			minNetRxBytes:        100000,
			minStorageIOPS:       10,
		},
		{
			name:                 "hv-04 coriolis-win-minion (without balloon stats)",
			fixtureFile:          "../../testdata/virt-handler-hv-04.prom",
			nodeFallback:         "hv-04",
			expectedVMKey:        "default/coriolis-win-minion",
			expectBalloon:        false,
			minCPUUsageSeconds:   100.0,
			minMemoryDomainBytes: 1024 * 1024 * 1024,
			minNetRxBytes:        1000,
			minStorageIOPS:       10,
		},
		{
			name:                 "hv-04 test2 (with balloon stats)",
			fixtureFile:          "../../testdata/virt-handler-hv-04.prom",
			nodeFallback:         "hv-04",
			expectedVMKey:        "default/test2",
			expectBalloon:        true,
			minCPUUsageSeconds:   100.0,
			minMemoryDomainBytes: 1024 * 1024 * 1024,
			minNetRxBytes:        10000,
			minStorageIOPS:       10,
		},
	}

	now := time.Now()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Clean(tc.fixtureFile)
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("failed to open fixture file %s: %v", tc.fixtureFile, err)
			}
			defer file.Close()

			samples, err := collect.ParseVirtHandlerMetrics(file, tc.nodeFallback, now)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			sample, ok := samples[tc.expectedVMKey]
			if !ok {
				t.Fatalf("expected VM %s not found in parsed samples. Available: %v", tc.expectedVMKey, sampleKeys(samples))
			}

			if sample.Node == "" {
				t.Errorf("expected node to be set, got empty")
			}

			if sample.CPUUsageSeconds < tc.minCPUUsageSeconds {
				t.Errorf("expected CPUUsageSeconds >= %f, got %f", tc.minCPUUsageSeconds, sample.CPUUsageSeconds)
			}

			if sample.MemoryDomainBytes < tc.minMemoryDomainBytes {
				t.Errorf("expected MemoryDomainBytes >= %d, got %d", tc.minMemoryDomainBytes, sample.MemoryDomainBytes)
			}

			if sample.HasBalloonStats != tc.expectBalloon {
				t.Errorf("expected HasBalloonStats=%v, got %v", tc.expectBalloon, sample.HasBalloonStats)
			}

			if sample.NetRxBytesTotal < tc.minNetRxBytes {
				t.Errorf("expected NetRxBytesTotal >= %d, got %d", tc.minNetRxBytes, sample.NetRxBytesTotal)
			}

			totalIOPS := sample.StorageReadIOPSTotal + sample.StorageWriteIOPSTotal
			if totalIOPS < tc.minStorageIOPS {
				t.Errorf("expected StorageIOPS >= %d, got %d", tc.minStorageIOPS, totalIOPS)
			}
		})
	}
}

func sampleKeys(m map[string]*collect.VMISample) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
