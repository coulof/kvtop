package record

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/coulof/kvtop/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var vmiGVR = schema.GroupVersionResource{
	Group:    "kubevirt.io",
	Version:  "v1",
	Resource: "virtualmachineinstances",
}

var promNodeRegex = regexp.MustCompile(`virt-handler-(.+)\.prom$`)

// RecordOptions configures the kvtop record subcommand.
type RecordOptions struct {
	OutputDir            string
	Duration             time.Duration
	Interval             time.Duration
	Anonymize            bool
	ReplayDir            string // Read from existing replay fixtures (for offline re-recording & testing)
	Kubeconfig           string
	VirtHandlerNamespace string
}

// Record captures cluster metrics, informer metadata, and saves them to OutputDir for --replay.
func Record(ctx context.Context, opts RecordOptions) error {
	if opts.OutputDir == "" {
		return fmt.Errorf("output directory is required (--out)")
	}
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed creating output directory: %w", err)
	}

	var anon *Anonymizer
	if opts.Anonymize {
		anon = NewAnonymizer()
	}

	// 1. Offline replay directory re-recording / anonymization
	if opts.ReplayDir != "" {
		return recordFromReplay(opts, anon)
	}

	// 2. Live cluster recording
	kClient, err := kube.NewClient(opts.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed connecting to cluster: %w", err)
	}

	// Capture VMI resource list
	vmiList, err := kClient.Dynamic.Resource(vmiGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed listing VMIs: %w", err)
	}

	if anon != nil {
		for i := range vmiList.Items {
			anon.AnonymizeVMI(&vmiList.Items[i])
		}
	}

	rawItems := make([]interface{}, len(vmiList.Items))
	for i, item := range vmiList.Items {
		rawItems[i] = item.Object
	}
	outObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "List",
		"items":      rawItems,
	}

	vmiBytes, err := json.MarshalIndent(outObj, "", "  ")
	if err != nil {
		return fmt.Errorf("failed marshaling VMIs: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "vmis.json"), vmiBytes, 0644); err != nil {
		return fmt.Errorf("failed writing vmis.json: %w", err)
	}

	// Discover and scrape virt-handler pods
	ns := opts.VirtHandlerNamespace
	if ns == "" {
		ns = "harvester-system"
	}

	handlers, err := kClient.ListVirtHandlers(ctx, ns)
	if err != nil {
		return fmt.Errorf("failed discovering virt-handler pods: %w", err)
	}
	if len(handlers) == 0 {
		return fmt.Errorf("no virt-handler pods found in namespace '%s'", ns)
	}

	for _, h := range handlers {
		rawProm, err := kClient.ProxyMetrics(ctx, h.Namespace, h.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed scraping pod %s on node %s: %v\n", h.Name, h.NodeName, err)
			continue
		}

		nodeName := h.NodeName
		promContent := string(rawProm)
		if anon != nil {
			nodeName = anon.AnonymizeNode(nodeName)
			promContent = anon.AnonymizePrometheusMetrics(promContent)
		}

		outFile := filepath.Join(opts.OutputDir, fmt.Sprintf("virt-handler-%s.prom", nodeName))
		if err := os.WriteFile(outFile, []byte(promContent), 0644); err != nil {
			return fmt.Errorf("failed writing %s: %w", outFile, err)
		}
	}

	return nil
}

func recordFromReplay(opts RecordOptions, anon *Anonymizer) error {
	// 1. Process vmis.json
	vmiPath := filepath.Join(opts.ReplayDir, "vmis.json")
	if data, err := os.ReadFile(vmiPath); err == nil {
		var list unstructured.UnstructuredList
		if err := json.Unmarshal(data, &list); err == nil {
			if anon != nil {
				for i := range list.Items {
					anon.AnonymizeVMI(&list.Items[i])
				}
			}
			rawItems := make([]interface{}, len(list.Items))
			for i, item := range list.Items {
				rawItems[i] = item.Object
			}
			outObj := map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"items":      rawItems,
			}
			outBytes, _ := json.MarshalIndent(outObj, "", "  ")
			_ = os.WriteFile(filepath.Join(opts.OutputDir, "vmis.json"), outBytes, 0644)
		}
	}

	// 2. Process all virt-handler-*.prom files
	matches, err := filepath.Glob(filepath.Join(opts.ReplayDir, "virt-handler-*.prom"))
	if err != nil {
		return err
	}

	for _, promFile := range matches {
		data, err := os.ReadFile(promFile)
		if err != nil {
			continue
		}

		base := filepath.Base(promFile)
		promContent := string(data)
		outNodeName := ""

		if sub := promNodeRegex.FindStringSubmatch(base); len(sub) == 2 {
			origNode := sub[1]
			if anon != nil {
				outNodeName = anon.AnonymizeNode(origNode)
				promContent = anon.AnonymizePrometheusMetrics(promContent)
			} else {
				outNodeName = origNode
			}
		}

		if outNodeName != "" {
			outFile := filepath.Join(opts.OutputDir, fmt.Sprintf("virt-handler-%s.prom", outNodeName))
			_ = os.WriteFile(outFile, []byte(promContent), 0644)
		}
	}

	// 3. Process virsh-domstats.txt if present
	domstatsPath := filepath.Join(opts.ReplayDir, "virsh-domstats.txt")
	if data, err := os.ReadFile(domstatsPath); err == nil {
		_ = os.WriteFile(filepath.Join(opts.OutputDir, "virsh-domstats.txt"), data, 0644)
	}

	return nil
}
