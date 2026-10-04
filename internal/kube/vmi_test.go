package kube_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/coulof/kvtop/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractVMIInfo(t *testing.T) {
	testdataFile := filepath.Join("..", "..", "testdata", "vmis.json")
	data, err := os.ReadFile(testdataFile)
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	var list unstructured.UnstructuredList
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("failed to unmarshal vmis.json: %v", err)
	}

	if len(list.Items) == 0 {
		t.Fatalf("expected non-empty VMI list in test fixture")
	}

	// Find coriolis-win-minion
	var target *unstructured.Unstructured
	for _, item := range list.Items {
		if item.GetName() == "coriolis-win-minion" {
			itemCopy := item
			target = &itemCopy
			break
		}
	}

	if target == nil {
		t.Fatalf("coriolis-win-minion not found in fixture")
	}

	info := kube.ExtractVMIInfo(target)

	if info.Name != "coriolis-win-minion" {
		t.Errorf("expected name coriolis-win-minion, got %s", info.Name)
	}
	if info.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", info.Namespace)
	}
	if info.NodeName != "hv-04" {
		t.Errorf("expected nodeName hv-04, got %s", info.NodeName)
	}
	if info.Phase != "Running" {
		t.Errorf("expected phase Running, got %s", info.Phase)
	}
	if info.CPUCores != 4 {
		t.Errorf("expected 4 CPU cores, got %d", info.CPUCores)
	}
	if info.CPUTopology.Cores != 4 || info.CPUTopology.Sockets != 1 || info.CPUTopology.Threads != 1 {
		t.Errorf("unexpected CPU topology: %+v", info.CPUTopology)
	}
	if info.MemoryGuest != "7936Mi" {
		t.Errorf("expected MemoryGuest 7936Mi, got %s", info.MemoryGuest)
	}
	if info.EvictionStrategy != "LiveMigrateIfPossible" {
		t.Errorf("expected EvictionStrategy LiveMigrateIfPossible, got %s", info.EvictionStrategy)
	}
	if len(info.Conditions) == 0 {
		t.Errorf("expected non-empty conditions")
	}
	if len(info.Volumes) == 0 {
		t.Errorf("expected non-empty volumes")
	}

	// Verify volume claim extraction
	foundRootdisk := false
	for _, v := range info.Volumes {
		if v.Name == "rootdisk" {
			foundRootdisk = true
			if v.ClaimName != "coriolis-win-minion-rootdisk" {
				t.Errorf("expected claimName coriolis-win-minion-rootdisk, got %s", v.ClaimName)
			}
			if v.Capacity != "64Gi" {
				t.Errorf("expected capacity 64Gi, got %s", v.Capacity)
			}
			if v.VolumeMode != "Block" {
				t.Errorf("expected volumeMode Block, got %s", v.VolumeMode)
			}
		}
	}
	if !foundRootdisk {
		t.Errorf("rootdisk volume not found in extracted volumes")
	}
}
