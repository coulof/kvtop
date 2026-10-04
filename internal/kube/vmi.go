package kube

import (
	"context"
	"encoding/json"
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

// CPUTopology represents the guest CPU topology.
type CPUTopology struct {
	Cores   int64 `json:"cores"`
	Sockets int64 `json:"sockets"`
	Threads int64 `json:"threads"`
}

// VMICondition represents a status condition on a VMI.
type VMICondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"last_transition_time,omitempty"`
}

// VMIVolumeInfo represents an attached volume and storage info.
type VMIVolumeInfo struct {
	Name         string `json:"name"`
	ClaimName    string `json:"claim_name,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	VolumeMode   string `json:"volume_mode,omitempty"`
	Capacity     string `json:"capacity,omitempty"`
}

// VMIInfo holds key metadata, sizing, and context for a VirtualMachineInstance.
type VMIInfo struct {
	Namespace             string
	Name                  string
	NodeName              string
	Phase                 string
	CPUCores              int64
	IP                    string
	CPUTopology           CPUTopology
	DedicatedCPUPlacement bool
	MemoryGuest           string
	MemoryRequested       string
	MemoryLimit           string
	InstanceType          string
	Preference            string
	EvictionStrategy      string
	Conditions            []VMICondition
	Volumes               []VMIVolumeInfo
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
	cores, foundCores := nestedInt64(u.Object, "spec", "domain", "cpu", "cores")
	if !foundCores || cores <= 0 {
		cores = 1
	}

	sockets, foundSockets := nestedInt64(u.Object, "spec", "domain", "cpu", "sockets")
	if !foundSockets || sockets <= 0 {
		sockets = 1
	}

	threads, foundThreads := nestedInt64(u.Object, "spec", "domain", "cpu", "threads")
	if !foundThreads || threads <= 0 {
		threads = 1
	}

	totalVCPUs := cores * sockets * threads

	// Dedicated CPU placement
	dedicated, _, _ := unstructured.NestedBool(u.Object, "spec", "domain", "cpu", "dedicatedCpuPlacement")

	// Memory specs
	memGuest, _, _ := unstructured.NestedString(u.Object, "spec", "domain", "memory", "guest")
	memReq, _, _ := unstructured.NestedString(u.Object, "spec", "domain", "resources", "requests", "memory")
	memLim, _, _ := unstructured.NestedString(u.Object, "spec", "domain", "resources", "limits", "memory")

	// Instance type & preference
	instType, _, _ := unstructured.NestedString(u.Object, "spec", "instancetype", "name")
	if instType == "" {
		instType, _, _ = unstructured.NestedString(u.Object, "metadata", "annotations", "vm.kubevirt.io/flavor")
	}
	pref, _, _ := unstructured.NestedString(u.Object, "spec", "preference", "name")

	// Eviction / Run strategy
	evictionStrategy, _, _ := unstructured.NestedString(u.Object, "spec", "evictionStrategy")

	// Conditions
	var conditions []VMICondition
	if conds, ok, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); ok {
		for _, item := range conds {
			if cMap, ok := item.(map[string]interface{}); ok {
				cType, _ := cMap["type"].(string)
				cStatus, _ := cMap["status"].(string)
				cReason, _ := cMap["reason"].(string)
				cMessage, _ := cMap["message"].(string)
				cTrans, _ := cMap["lastTransitionTime"].(string)
				conditions = append(conditions, VMICondition{
					Type:               cType,
					Status:             cStatus,
					Reason:             cReason,
					Message:            cMessage,
					LastTransitionTime: cTrans,
				})
			}
		}
	}

	// Status volume map for capacity, volumeMode, storageClass
	volStatusMap := make(map[string]map[string]interface{})
	if volStatuses, ok, _ := unstructured.NestedSlice(u.Object, "status", "volumeStatus"); ok {
		for _, vsItem := range volStatuses {
			if vsMap, ok := vsItem.(map[string]interface{}); ok {
				if vsName, ok := vsMap["name"].(string); ok {
					volStatusMap[vsName] = vsMap
				}
			}
		}
	}

	// Volumes
	var volumes []VMIVolumeInfo
	if specVols, ok, _ := unstructured.NestedSlice(u.Object, "spec", "volumes"); ok {
		for _, item := range specVols {
			if vMap, ok := item.(map[string]interface{}); ok {
				vName, _ := vMap["name"].(string)
				vInfo := VMIVolumeInfo{Name: vName}
				if pvc, ok := vMap["persistentVolumeClaim"].(map[string]interface{}); ok {
					vInfo.ClaimName, _ = pvc["claimName"].(string)
				} else if cd, ok := vMap["containerDisk"].(map[string]interface{}); ok {
					vInfo.ClaimName, _ = cd["image"].(string)
				}
				if vs, ok := volStatusMap[vName]; ok {
					if pvcInfo, ok := vs["persistentVolumeClaimInfo"].(map[string]interface{}); ok {
						if vInfo.ClaimName == "" {
							vInfo.ClaimName, _ = pvcInfo["claimName"].(string)
						}
						vInfo.VolumeMode, _ = pvcInfo["volumeMode"].(string)
						vInfo.StorageClass, _ = pvcInfo["storageClassName"].(string)
						if capMap, ok := pvcInfo["capacity"].(map[string]interface{}); ok {
							vInfo.Capacity, _ = capMap["storage"].(string)
						}
					}
				}
				volumes = append(volumes, vInfo)
			}
		}
	}

	return VMIInfo{
		Namespace:             ns,
		Name:                  name,
		NodeName:              nodeName,
		Phase:                 phase,
		CPUCores:              totalVCPUs,
		IP:                    ip,
		CPUTopology: CPUTopology{
			Cores:   cores,
			Sockets: sockets,
			Threads: threads,
		},
		DedicatedCPUPlacement: dedicated,
		MemoryGuest:           memGuest,
		MemoryRequested:       memReq,
		MemoryLimit:           memLim,
		InstanceType:          instType,
		Preference:            pref,
		EvictionStrategy:      evictionStrategy,
		Conditions:            conditions,
		Volumes:               volumes,
	}
}

func nestedInt64(obj map[string]interface{}, fields ...string) (int64, bool) {
	val, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return 0, false
	}
	switch v := val.(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}
