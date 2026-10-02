package kube_test

import (
	"testing"

	"github.com/coulof/kvtop/internal/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractMigrationInfo(t *testing.T) {
	tests := []struct {
		name          string
		raw           map[string]interface{}
		expectActive  bool
		expectedVMI   string
		expectedPhase string
		expectedSrc   string
		expectedTgt   string
	}{
		{
			name: "active migration in running phase",
			raw: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test-mig",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"vmiName": "vm-production",
				},
				"status": map[string]interface{}{
					"phase": "Running",
					"migrationState": map[string]interface{}{
						"sourceNode": "hv-01",
						"targetNode": "hv-04",
						"completed":  false,
						"failed":     false,
					},
				},
			},
			expectActive:  true,
			expectedVMI:   "vm-production",
			expectedPhase: "Running",
			expectedSrc:   "hv-01",
			expectedTgt:   "hv-04",
		},
		{
			name: "completed migration",
			raw: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "test-mig-done",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"vmiName": "vm-production",
				},
				"status": map[string]interface{}{
					"phase": "Succeeded",
					"migrationState": map[string]interface{}{
						"sourceNode": "hv-01",
						"targetNode": "hv-04",
						"completed":  true,
						"failed":     false,
					},
				},
			},
			expectActive:  false,
			expectedVMI:   "vm-production",
			expectedPhase: "Succeeded",
			expectedSrc:   "hv-01",
			expectedTgt:   "hv-04",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: tt.raw}
			info := kube.ExtractMigrationInfo(u)

			if info.Active != tt.expectActive {
				t.Errorf("expected Active=%v, got %v", tt.expectActive, info.Active)
			}
			if info.VMIName != tt.expectedVMI {
				t.Errorf("expected VMIName=%s, got %s", tt.expectedVMI, info.VMIName)
			}
			if info.Phase != tt.expectedPhase {
				t.Errorf("expected Phase=%s, got %s", tt.expectedPhase, info.Phase)
			}
			if info.SourceNode != tt.expectedSrc {
				t.Errorf("expected SourceNode=%s, got %s", tt.expectedSrc, info.SourceNode)
			}
			if info.TargetNode != tt.expectedTgt {
				t.Errorf("expected TargetNode=%s, got %s", tt.expectedTgt, info.TargetNode)
			}
		})
	}
}
