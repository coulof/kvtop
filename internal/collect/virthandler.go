package collect

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coulof/kvtop/internal/kube"
)

// VirtHandlerCollector fetches and aggregates metrics from virt-handler pods across all nodes.
type VirtHandlerCollector struct {
	client    *kube.Client
	namespace string
	timeout   time.Duration
}

// NewVirtHandlerCollector creates a collector instance.
func NewVirtHandlerCollector(client *kube.Client, namespace string, timeout time.Duration) *VirtHandlerCollector {
	if namespace == "" {
		namespace = "harvester-system"
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &VirtHandlerCollector{
		client:    client,
		namespace: namespace,
		timeout:   timeout,
	}
}

// Name returns the collector identifier.
func (c *VirtHandlerCollector) Name() string {
	return "virt-handler"
}

// Collect queries all virt-handler pods in parallel via the API server pod proxy.
func (c *VirtHandlerCollector) Collect(ctx context.Context) ([]VMISample, error) {
	pods, err := c.client.ListVirtHandlers(ctx, c.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to discover virt-handler pods: %w", err)
	}

	if len(pods) == 0 {
		return nil, fmt.Errorf("no virt-handler pods found in namespace %s", c.namespace)
	}

	now := time.Now()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		samples []VMISample
		errs    []error
	)

	wg.Add(len(pods))
	for _, p := range pods {
		go func(pod kube.VirtHandlerPod) {
			defer wg.Done()

			reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()

			raw, err := c.client.ProxyMetrics(reqCtx, pod.Namespace, pod.Name)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("node %s (%s): %w", pod.NodeName, pod.Name, err))
				mu.Unlock()
				return
			}

			parsed, err := ParseVirtHandlerMetrics(bytes.NewReader(raw), pod.NodeName, now)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("parse node %s (%s): %w", pod.NodeName, pod.Name, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			for _, s := range parsed {
				samples = append(samples, *s)
			}
			mu.Unlock()
		}(p)
	}

	wg.Wait()

	// If all nodes failed, return an aggregated error
	if len(samples) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all %d virt-handler scrapes failed: %v", len(pods), errs[0])
	}

	return samples, nil
}
