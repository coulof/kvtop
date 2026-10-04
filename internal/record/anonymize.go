package record

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Anonymizer manages consistent pseudonym mapping across all recording artifacts.
type Anonymizer struct {
	mu         sync.Mutex
	namespaces map[string]string
	vms        map[string]string
	nodes      map[string]string
	volumes    map[string]string
	ips        map[string]string
	macs       map[string]string
}

// NewAnonymizer creates an initialized Anonymizer.
func NewAnonymizer() *Anonymizer {
	return &Anonymizer{
		namespaces: make(map[string]string),
		vms:        make(map[string]string),
		nodes:      make(map[string]string),
		volumes:    make(map[string]string),
		ips:        make(map[string]string),
		macs:       make(map[string]string),
	}
}

// AnonymizeNamespace returns a stable pseudonym for a namespace (e.g. ns-1).
func (a *Anonymizer) AnonymizeNamespace(ns string) string {
	if ns == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.namespaces[ns]; exists {
		return anon
	}
	anon := fmt.Sprintf("ns-%d", len(a.namespaces)+1)
	a.namespaces[ns] = anon
	return anon
}

// AnonymizeVM returns a stable pseudonym for a VM name (e.g. vm-1).
func (a *Anonymizer) AnonymizeVM(name string) string {
	if name == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.vms[name]; exists {
		return anon
	}
	anon := fmt.Sprintf("vm-%d", len(a.vms)+1)
	a.vms[name] = anon
	return anon
}

// AnonymizeNode returns a stable pseudonym for a node name (e.g. node-1).
func (a *Anonymizer) AnonymizeNode(node string) string {
	if node == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.nodes[node]; exists {
		return anon
	}
	anon := fmt.Sprintf("node-%d", len(a.nodes)+1)
	a.nodes[node] = anon
	return anon
}

// AnonymizeVolume returns a stable pseudonym for a volume or PVC name (e.g. vol-1).
func (a *Anonymizer) AnonymizeVolume(vol string) string {
	if vol == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.volumes[vol]; exists {
		return anon
	}
	anon := fmt.Sprintf("vol-%d", len(a.volumes)+1)
	a.volumes[vol] = anon
	return anon
}

// AnonymizeIP returns a stable fake IP address.
func (a *Anonymizer) AnonymizeIP(ip string) string {
	if ip == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.ips[ip]; exists {
		return anon
	}
	anon := fmt.Sprintf("10.0.0.%d", len(a.ips)+1)
	a.ips[ip] = anon
	return anon
}

// AnonymizeMAC returns a stable fake MAC address.
func (a *Anonymizer) AnonymizeMAC(mac string) string {
	if mac == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if anon, exists := a.macs[mac]; exists {
		return anon
	}
	anon := fmt.Sprintf("02:00:00:00:00:%02x", len(a.macs)+1)
	a.macs[mac] = anon
	return anon
}

// AnonymizeVMI strips sensitive metadata, labels, and replaces resource names with pseudonyms.
func (a *Anonymizer) AnonymizeVMI(u *unstructured.Unstructured) {
	oldName := u.GetName()
	oldNS := u.GetNamespace()

	newName := a.AnonymizeVM(oldName)
	newNS := a.AnonymizeNamespace(oldNS)

	u.SetName(newName)
	u.SetNamespace(newNS)

	nodeName, _, _ := unstructured.NestedString(u.Object, "status", "nodeName")
	if nodeName == "" {
		nodeName, _, _ = unstructured.NestedString(u.Object, "metadata", "labels", "kubevirt.io/nodeName")
	}
	newNodeName := a.AnonymizeNode(nodeName)

	// Drop sensitive annotations and strip labels down to system node reference
	u.SetAnnotations(nil)
	u.SetLabels(map[string]string{
		"kubevirt.io/nodeName":   newNodeName,
		"harvesterhci.io/vmName": newName,
	})

	// Clear cluster-specific internal fields
	delete(u.Object["metadata"].(map[string]interface{}), "uid")
	delete(u.Object["metadata"].(map[string]interface{}), "resourceVersion")
	delete(u.Object["metadata"].(map[string]interface{}), "generation")
	delete(u.Object["metadata"].(map[string]interface{}), "finalizers")
	delete(u.Object["metadata"].(map[string]interface{}), "managedFields")
	delete(u.Object["metadata"].(map[string]interface{}), "ownerReferences")

	// Anonymize spec.volumes
	if specVols, ok, _ := unstructured.NestedSlice(u.Object, "spec", "volumes"); ok {
		for _, item := range specVols {
			if vMap, ok := item.(map[string]interface{}); ok {
				if vName, ok := vMap["name"].(string); ok {
					vMap["name"] = a.AnonymizeVolume(vName)
				}
				if pvc, ok := vMap["persistentVolumeClaim"].(map[string]interface{}); ok {
					if cName, ok := pvc["claimName"].(string); ok {
						pvc["claimName"] = a.AnonymizeVolume(cName)
					}
				}
			}
		}
		_ = unstructured.SetNestedSlice(u.Object, specVols, "spec", "volumes")
	}

	// Anonymize spec interfaces
	if ifaces, ok, _ := unstructured.NestedSlice(u.Object, "spec", "domain", "devices", "interfaces"); ok {
		for _, item := range ifaces {
			if ifaceMap, ok := item.(map[string]interface{}); ok {
				if mac, ok := ifaceMap["macAddress"].(string); ok {
					ifaceMap["macAddress"] = a.AnonymizeMAC(mac)
				}
			}
		}
		_ = unstructured.SetNestedSlice(u.Object, ifaces, "spec", "domain", "devices", "interfaces")
	}

	// Anonymize status
	if newNodeName != "" {
		_ = unstructured.SetNestedField(u.Object, newNodeName, "status", "nodeName")
	}

	if statusIfaces, ok, _ := unstructured.NestedSlice(u.Object, "status", "interfaces"); ok {
		for _, item := range statusIfaces {
			if ifaceMap, ok := item.(map[string]interface{}); ok {
				if ip, ok := ifaceMap["ipAddress"].(string); ok {
					ifaceMap["ipAddress"] = a.AnonymizeIP(ip)
				}
				if ipList, ok := ifaceMap["ipAddresses"].([]interface{}); ok {
					var newIPs []interface{}
					for _, ipItem := range ipList {
						if ipStr, ok := ipItem.(string); ok {
							newIPs = append(newIPs, a.AnonymizeIP(ipStr))
						}
					}
					ifaceMap["ipAddresses"] = newIPs
				}
				if mac, ok := ifaceMap["mac"].(string); ok {
					ifaceMap["mac"] = a.AnonymizeMAC(mac)
				}
			}
		}
		_ = unstructured.SetNestedSlice(u.Object, statusIfaces, "status", "interfaces")
	}

	if volStatuses, ok, _ := unstructured.NestedSlice(u.Object, "status", "volumeStatus"); ok {
		for _, item := range volStatuses {
			if vsMap, ok := item.(map[string]interface{}); ok {
				if vsName, ok := vsMap["name"].(string); ok {
					vsMap["name"] = a.AnonymizeVolume(vsName)
				}
				if pvcInfo, ok := vsMap["persistentVolumeClaimInfo"].(map[string]interface{}); ok {
					if cName, ok := pvcInfo["claimName"].(string); ok {
						pvcInfo["claimName"] = a.AnonymizeVolume(cName)
					}
				}
			}
		}
		_ = unstructured.SetNestedSlice(u.Object, volStatuses, "status", "volumeStatus")
	}
}

var (
	labelRegex = regexp.MustCompile(`([a-zA-Z_0-9]+)="([^"]*)"`)
)

// AnonymizePrometheusMetrics rewrites label values in a Prometheus text scrape.
func (a *Anonymizer) AnonymizePrometheusMetrics(content string) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Preserve comments / metadata
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			sb.WriteString(line)
			sb.WriteByte('\n')
			continue
		}

		// Metric line: format name{label1="v1",label2="v2"} value [timestamp]
		openIdx := strings.IndexByte(line, '{')
		closeIdx := strings.IndexByte(line, '}')

		if openIdx != -1 && closeIdx != -1 && closeIdx > openIdx {
			metricName := line[:openIdx]
			labelPart := line[openIdx+1 : closeIdx]
			valPart := line[closeIdx+1:]

			// Rewrite labels
			matches := labelRegex.FindAllStringSubmatch(labelPart, -1)
			var newLabels []string

			for _, m := range matches {
				k := m[1]
				v := m[2]

				// Drop noisy or sensitive user labels
				if strings.HasPrefix(k, "kubernetes_vmi_label_") {
					continue
				}

				switch k {
				case "namespace":
					v = a.AnonymizeNamespace(v)
				case "name":
					v = a.AnonymizeVM(v)
				case "node":
					v = a.AnonymizeNode(v)
				case "drive":
					v = a.AnonymizeVolume(v)
				}

				newLabels = append(newLabels, fmt.Sprintf(`%s="%s"`, k, v))
			}

			sb.WriteString(metricName)
			sb.WriteByte('{')
			sb.WriteString(strings.Join(newLabels, ","))
			sb.WriteByte('}')
			sb.WriteString(valPart)
			sb.WriteByte('\n')
		} else {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
