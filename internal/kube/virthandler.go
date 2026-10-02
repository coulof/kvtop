package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtHandlerPod represents a discovered virt-handler instance on a cluster node.
type VirtHandlerPod struct {
	Name      string
	Namespace string
	NodeName  string
	IP        string
}

// ListVirtHandlers finds all Running virt-handler daemon pods in the specified namespace
// (defaults to "harvester-system" if empty).
func (c *Client) ListVirtHandlers(ctx context.Context, namespace string) ([]VirtHandlerPod, error) {
	if namespace == "" {
		namespace = "harvester-system"
	}

	pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "kubevirt.io=virt-handler",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list virt-handler pods in namespace %s: %w", namespace, err)
	}

	var handlers []VirtHandlerPod
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		handlers = append(handlers, VirtHandlerPod{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			NodeName:  pod.Spec.NodeName,
			IP:        pod.Status.PodIP,
		})
	}

	return handlers, nil
}

// ProxyMetrics fetches the raw /metrics payload from a virt-handler pod via the
// API server's secure pod proxy (https:<pod>:8443/proxy/metrics).
func (c *Client) ProxyMetrics(ctx context.Context, namespace, podName string) ([]byte, error) {
	raw, err := c.Clientset.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		Name(fmt.Sprintf("https:%s:8443", podName)).
		SubResource("proxy").
		Suffix("metrics").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("proxy metrics failed for pod %s/%s: %w", namespace, podName, err)
	}
	return raw, nil
}
