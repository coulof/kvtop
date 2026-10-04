package record_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/record"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAnonymizer_StableMapping(t *testing.T) {
	anon := record.NewAnonymizer()

	// Same input should return identical output
	vm1 := anon.AnonymizeVM("production-db")
	vm2 := anon.AnonymizeVM("production-db")
	if vm1 != vm2 {
		t.Errorf("expected stable VM mapping: %s vs %s", vm1, vm2)
	}

	// Different input should return different output
	vm3 := anon.AnonymizeVM("staging-web")
	if vm1 == vm3 {
		t.Errorf("expected different pseudonym for staging-web, got %s", vm3)
	}

	// Namespaces
	ns1 := anon.AnonymizeNamespace("default")
	ns2 := anon.AnonymizeNamespace("default")
	if ns1 != ns2 {
		t.Errorf("expected stable namespace mapping: %s vs %s", ns1, ns2)
	}

	// Nodes
	node1 := anon.AnonymizeNode("hv-01")
	node2 := anon.AnonymizeNode("hv-01")
	if node1 != node2 {
		t.Errorf("expected stable node mapping: %s vs %s", node1, node2)
	}

	// IPs
	ip1 := anon.AnonymizeIP("192.168.1.50")
	ip2 := anon.AnonymizeIP("192.168.1.50")
	if ip1 != ip2 {
		t.Errorf("expected stable IP mapping: %s vs %s", ip1, ip2)
	}
}

func TestAnonymizePrometheusMetrics(t *testing.T) {
	anon := record.NewAnonymizer()
	rawProm := `# HELP kubevirt_vmi_cpu_usage_seconds_total Total CPU time.
# TYPE kubevirt_vmi_cpu_usage_seconds_total counter
kubevirt_vmi_cpu_usage_seconds_total{drive="rootdisk",kubernetes_vmi_label_user_email="admin@example.com",name="my-vm",namespace="prod",node="hv-01"} 123.45
`

	anonymized := anon.AnonymizePrometheusMetrics(rawProm)

	// User labels should be dropped
	if strings.Contains(anonymized, "admin@example.com") {
		t.Errorf("expected sensitive user label to be dropped, got:\n%s", anonymized)
	}
	// Identifiers should be replaced
	if strings.Contains(anonymized, "my-vm") || strings.Contains(anonymized, "prod") || strings.Contains(anonymized, "hv-01") {
		t.Errorf("expected identifiers to be anonymized, got:\n%s", anonymized)
	}
	if !strings.Contains(anonymized, `name="vm-1"`) || !strings.Contains(anonymized, `namespace="ns-1"`) || !strings.Contains(anonymized, `node="node-1"`) {
		t.Errorf("expected pseudonyms in metric line, got:\n%s", anonymized)
	}
}

func TestRecord_ReplayAndAnonymize(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	tempOut, err := os.MkdirTemp("", "kvtop-record-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempOut)

	err = record.Record(context.Background(), record.RecordOptions{
		OutputDir: tempOut,
		ReplayDir: testdataDir,
		Anonymize: true,
	})
	if err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Verify vmis.json exists in tempOut
	vmiFile := filepath.Join(tempOut, "vmis.json")
	if _, err := os.Stat(vmiFile); os.IsNotExist(err) {
		t.Fatalf("vmis.json was not written to output dir")
	}

	// Verify LoadReplayVMIs can parse the anonymized output
	anonVMIs, err := collect.LoadReplayVMIs(tempOut)
	if err != nil {
		t.Fatalf("LoadReplayVMIs failed on anonymized output: %v", err)
	}
	if len(anonVMIs) == 0 {
		t.Fatalf("expected at least 1 VMI in anonymized output")
	}

	// Ensure names are pseudonyms
	for _, v := range anonVMIs {
		if strings.Contains(v.Name, "coriolis") || strings.Contains(v.Name, "tumbleweed") {
			t.Errorf("real VM name '%s' leaked into anonymized recording", v.Name)
		}
		if !strings.HasPrefix(v.Name, "vm-") {
			t.Errorf("expected pseudonym prefix 'vm-', got %s", v.Name)
		}
	}

	// Verify ReplayCollector can load and collect from anonymized directory
	rc, err := collect.NewReplayCollector(tempOut, 2*time.Second)
	if err != nil {
		t.Fatalf("NewReplayCollector failed on anonymized output: %v", err)
	}
	samples, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect failed on anonymized replay: %v", err)
	}
	if len(samples) == 0 {
		t.Fatalf("expected non-empty samples from anonymized replay")
	}
}

func TestAnonymizeVMI_StripSensitiveFields(t *testing.T) {
	anon := record.NewAnonymizer()
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "secret-vm",
				"namespace": "classified",
				"uid":       "12345-67890",
				"annotations": map[string]interface{}{
					"harvesterhci.io/secretData": "top-secret",
				},
				"labels": map[string]interface{}{
					"env":                  "production",
					"kubevirt.io/nodeName": "node-alpha",
				},
			},
			"spec": map[string]interface{}{
				"volumes": []interface{}{
					map[string]interface{}{
						"name": "data-volume",
						"persistentVolumeClaim": map[string]interface{}{
							"claimName": "secret-pvc",
						},
					},
				},
			},
		},
	}

	anon.AnonymizeVMI(u)

	if u.GetName() != "vm-1" {
		t.Errorf("expected name vm-1, got %s", u.GetName())
	}
	if u.GetNamespace() != "ns-1" {
		t.Errorf("expected namespace ns-1, got %s", u.GetNamespace())
	}
	if u.GetAnnotations() != nil {
		t.Errorf("expected annotations to be nil, got: %v", u.GetAnnotations())
	}
	if val, _, _ := unstructured.NestedString(u.Object, "metadata", "uid"); val != "" {
		t.Errorf("expected uid to be removed")
	}
}
