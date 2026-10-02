package collect

import (
	"fmt"
	"io"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// ParseVirtHandlerMetrics parses Prometheus text exposition format metrics from virt-handler.
func ParseVirtHandlerMetrics(r io.Reader, nodeFallback string, timestamp time.Time) (map[string]*VMISample, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	mfMap, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse prometheus metrics: %w", err)
	}

	samples := make(map[string]*VMISample)

	getOrCreateSample := func(ns, name, node string) *VMISample {
		key := fmt.Sprintf("%s/%s", ns, name)
		s, exists := samples[key]
		if !exists {
			if node == "" {
				node = nodeFallback
			}
			s = &VMISample{
				Namespace: ns,
				Name:      name,
				Node:      node,
				Timestamp: timestamp,
			}
			samples[key] = s
		} else if s.Node == "" && node != "" {
			s.Node = node
		}
		return s
	}

	for mfName, mf := range mfMap {
		switch mfName {
		case "kubevirt_vmi_cpu_usage_seconds_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.CPUUsageSeconds = getMetricValue(m)
			}

		case "kubevirt_vmi_memory_domain_bytes":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.MemoryDomainBytes = uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_memory_usable_bytes":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.MemoryUsableBytes = uint64(getMetricValue(m))
				s.HasBalloonStats = true
			}

		case "kubevirt_vmi_memory_available_bytes":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.MemoryAvailableBytes = uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_memory_unused_bytes":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.MemoryUnusedBytes = uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_memory_resident_bytes":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.MemoryResidentBytes = uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_network_receive_bytes_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.NetRxBytesTotal += uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_network_transmit_bytes_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.NetTxBytesTotal += uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_storage_iops_read_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.StorageReadIOPSTotal += uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_storage_iops_write_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.StorageWriteIOPSTotal += uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_storage_read_traffic_bytes_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.StorageReadBytesTotal += uint64(getMetricValue(m))
			}

		case "kubevirt_vmi_storage_write_traffic_bytes_total":
			for _, m := range mf.GetMetric() {
				ns, name, node := extractLabels(m)
				if name == "" {
					continue
				}
				s := getOrCreateSample(ns, name, node)
				s.StorageWriteBytesTotal += uint64(getMetricValue(m))
			}
		}
	}

	return samples, nil
}

func extractLabels(m *dto.Metric) (namespace, name, node string) {
	for _, lp := range m.GetLabel() {
		switch lp.GetName() {
		case "namespace":
			namespace = lp.GetValue()
		case "name":
			name = lp.GetValue()
		case "node":
			node = lp.GetValue()
		}
	}
	if namespace == "" {
		namespace = "default"
	}
	return namespace, name, node
}

func getMetricValue(m *dto.Metric) float64 {
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	if m.Untyped != nil {
		return m.Untyped.GetValue()
	}
	return 0
}
