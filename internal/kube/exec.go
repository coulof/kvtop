package kube

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// FindLauncherPod finds the running virt-launcher pod for a given VMI name and namespace.
func (c *Client) FindLauncherPod(ctx context.Context, namespace, vmiName string) (string, error) {
	selectors := []string{
		fmt.Sprintf("kubevirt.io=virt-launcher,vm.kubevirt.io/name=%s", vmiName),
		fmt.Sprintf("kubevirt.io=virt-launcher,vmi.kubevirt.io/id=%s", vmiName),
		fmt.Sprintf("kubevirt.io=virt-launcher,harvesterhci.io/vmName=%s", vmiName),
	}

	for _, sel := range selectors {
		pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: sel,
		})
		if err == nil && len(pods.Items) > 0 {
			for _, p := range pods.Items {
				if p.Status.Phase == corev1.PodRunning {
					return p.Name, nil
				}
			}
			return pods.Items[0].Name, nil
		}
	}

	return "", fmt.Errorf("no virt-launcher pod found for VMI %s/%s", namespace, vmiName)
}

// StreamVirshDomstatsExec opens a long-lived streaming exec into the virt-launcher pod's
// compute container, executing a 1s loop of `virsh domstats`.
// It returns an io.ReadCloser streaming the output. Closing ctx terminates the stream.
func (c *Client) StreamVirshDomstatsExec(ctx context.Context, namespace, podName string) (io.ReadCloser, error) {
	cmd := []string{"sh", "-c", "while true; do virsh domstats; sleep 1; done"}

	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "compute",
			Command:   cmd,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.RestConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to create SPDY executor for %s/%s: %w", namespace, podName, err)
	}

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		_ = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: pw,
			Stderr: io.Discard,
		})
	}()

	return pr, nil
}
