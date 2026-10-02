package kube

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var vmiGVR = schema.GroupVersionResource{
	Group:    "kubevirt.io",
	Version:  "v1",
	Resource: "virtualmachineinstances",
}

// VMIInfo holds key metadata and sizing for a VirtualMachineInstance.
type VMIInfo struct {
	Namespace string
	Name      string
	NodeName  string
	Phase     string
	CPUCores  int64
	IP        string
}

// ListVMIs retrieves all VMIs across all namespaces and extracts core count and node placement.
func (c *Client) ListVMIs(ctx context.Context) ([]VMIInfo, error) {
	list, err := c.Dynamic.Resource(vmiGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list VMIs: %w", err)
	}

	var results []VMIInfo
	for _, item := range list.Items {
		info := ExtractVMIInfo(&item)
		results = append(results, info)
	}
	return results, nil
}

// ExtractVMIInfo parses relevant metadata and sizing from an unstructured VMI object.
func ExtractVMIInfo(u *unstructured.Unstructured) VMIInfo {
	ns := u.GetNamespace()
	name := u.GetName()

	nodeName, _, _ := unstructured.NestedString(u.Object, "status", "nodeName")
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")

	// Extract primary IP address from status.interfaces
	var ip string
	if ifaces, ok, _ := unstructured.NestedSlice(u.Object, "status", "interfaces"); ok {
		for _, item := range ifaces {
			if ifaceMap, ok := item.(map[string]interface{}); ok {
				if ipAddr, ok := ifaceMap["ipAddress"].(string); ok && ipAddr != "" {
					ip = ipAddr
					break
				}
				if ipAddrs, ok := ifaceMap["ipAddresses"].([]interface{}); ok && len(ipAddrs) > 0 {
					for _, addrItem := range ipAddrs {
						if addrStr, ok := addrItem.(string); ok && addrStr != "" && !strings.Contains(addrStr, ":") {
							ip = addrStr
							break
						}
					}
					if ip != "" {
						break
					}
				}
			}
		}
	}

	// Calculate allotted vCPUs: sockets * cores * threads
	cores, foundCores, _ := unstructured.NestedInt64(u.Object, "spec", "domain", "cpu", "cores")
	if !foundCores || cores <= 0 {
		cores = 1
	}

	sockets, foundSockets, _ := unstructured.NestedInt64(u.Object, "spec", "domain", "cpu", "sockets")
	if !foundSockets || sockets <= 0 {
		sockets = 1
	}

	threads, foundThreads, _ := unstructured.NestedInt64(u.Object, "spec", "domain", "cpu", "threads")
	if !foundThreads || threads <= 0 {
		threads = 1
	}

	totalVCPUs := cores * sockets * threads

	return VMIInfo{
		Namespace: ns,
		Name:      name,
		NodeName:  nodeName,
		Phase:     phase,
		CPUCores:  totalVCPUs,
		IP:        ip,
	}
}
