package collect_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
)

func TestParseVirshBlock_Fixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/virsh-domstats.txt")
	if err != nil {
		t.Fatalf("failed to read testdata/virsh-domstats.txt: %v", err)
	}

	stats := collect.ParseVirshBlock(string(data), time.Now())

	if stats.DomainName != "default_spike-it" {
		t.Errorf("expected domain name 'default_spike-it', got %s", stats.DomainName)
	}

	if stats.CPUTimeNs == 0 {
		t.Errorf("expected non-zero CPUTimeNs")
	}

	// Balloon & RSS
	if stats.BalloonRSSBytes == 0 {
		t.Errorf("expected non-zero BalloonRSSBytes")
	}
	expectedRSS := uint64(962468 * 1024)
	if stats.BalloonRSSBytes != expectedRSS {
		t.Errorf("expected RSS=%d, got %d", expectedRSS, stats.BalloonRSSBytes)
	}

	// VCPUs
	if len(stats.VCPUs) != 2 {
		t.Fatalf("expected 2 VCPUs, got %d", len(stats.VCPUs))
	}
	if stats.VCPUs[0].ID != 0 || stats.VCPUs[0].TimeNs == 0 || stats.VCPUs[0].DelayNs == 0 {
		t.Errorf("unexpected vCPU 0 stats: %+v", stats.VCPUs[0])
	}

	// NICs
	if len(stats.NICs) != 1 {
		t.Fatalf("expected 1 NIC, got %d", len(stats.NICs))
	}
	if stats.NICs[0].Name != "tap37a8eec1ce1" || stats.NICs[0].RxBytes == 0 {
		t.Errorf("unexpected NIC stats: %+v", stats.NICs[0])
	}

	// Disks
	if len(stats.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(stats.Disks))
	}
	if stats.Disks[0].Name != "vda" || stats.Disks[0].ReadBytes == 0 || stats.Disks[0].CapacityBytes == 0 {
		t.Errorf("unexpected disk 0 stats: %+v", stats.Disks[0])
	}
}

func TestStreamVirshDomstats_MultiBlock(t *testing.T) {
	data, err := os.ReadFile("../../testdata/virsh-domstats.txt")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	// Concatenate two snapshots
	twoBlocks := string(data) + "\n" + string(data)
	r := strings.NewReader(twoBlocks)

	out := make(chan collect.VirshStats, 5)
	stopCh := make(chan struct{})

	go func() {
		collect.StreamVirshDomstats(r, out, stopCh)
		close(out)
	}()

	count := 0
	for s := range out {
		if s.DomainName != "default_spike-it" {
			t.Errorf("unexpected domain: %s", s.DomainName)
		}
		count++
	}

	if count != 2 {
		t.Errorf("expected 2 streamed snapshots, got %d", count)
	}
}
