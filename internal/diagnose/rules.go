package diagnose

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/ui"
)

// Rule identifiers as specified in AGENTS.md.
const (
	RuleCPUSaturated           = "cpu_saturated"
	RuleMemGuestPressure       = "mem_guest_pressure"
	RuleDiskLatencyHigh        = "disk_latency_high"
	RuleMigrationNotConverging = "migration_not_converging"
	RuleNodeImbalance          = "node_imbalance"
	RuleNodeOvercommit         = "node_overcommit"
	RuleMetricsMissing         = "metrics_missing"
)

// evaluateCPUSaturated checks if VM CPU usage exceeds saturation threshold.
func evaluateCPUSaturated(vm query.VMItem, th Thresholds, windowSec float64) *Finding {
	if vm.CPUSaturationPercent == nil || vm.AllottedVCPUs <= 0 {
		return nil
	}

	sat := vm.CPUSaturationPercent.Avg
	thPct := th.CPUSaturationRatio * 100.0

	if sat >= thPct {
		sev := SeverityWarning
		if sat >= 98.0 {
			sev = SeverityCritical
		}

		coresUsed := 0.0
		if vm.CPUCoresUsed != nil {
			coresUsed = vm.CPUCoresUsed.Avg
		}

		return &Finding{
			ID:       RuleCPUSaturated,
			Severity: sev,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("VM %s/%s CPU is saturated at %.1f%% of %d allotted vCPUs over the %.0fs window",
				vm.Namespace, vm.Name, sat, vm.AllottedVCPUs, windowSec),
			Evidence: map[string]interface{}{
				"cores_used_avg":     coresUsed,
				"allotted_vcpus":     vm.AllottedVCPUs,
				"saturation_percent": sat,
				"threshold_ratio":    th.CPUSaturationRatio,
			},
			WindowSeconds: windowSec,
		}
	}
	return nil
}

// evaluateMemGuestPressure checks if balloon guest memory usage exceeds threshold.
func evaluateMemGuestPressure(vm query.VMItem, th Thresholds, windowSec float64) *Finding {
	if vm.MemGuestUsedPercent == nil || vm.MemGuestUsedBytes == nil || vm.MemGuestTotalBytes == nil {
		return nil
	}

	usedPct := vm.MemGuestUsedPercent.Avg
	thPct := th.MemGuestPressureRatio * 100.0

	if usedPct >= thPct {
		sev := SeverityWarning
		if usedPct >= 95.0 {
			sev = SeverityCritical
		}

		return &Finding{
			ID:       RuleMemGuestPressure,
			Severity: sev,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("VM %s/%s guest memory is under pressure at %.1f%% used (%s / %s)",
				vm.Namespace, vm.Name, usedPct,
				ui.FormatBytes(vm.MemGuestUsedBytes.Avg),
				ui.FormatBytes(float64(*vm.MemGuestTotalBytes))),
			Evidence: map[string]interface{}{
				"memory_used_bytes":  vm.MemGuestUsedBytes.Avg,
				"memory_total_bytes": *vm.MemGuestTotalBytes,
				"used_percent":       usedPct,
				"threshold_ratio":    th.MemGuestPressureRatio,
			},
			WindowSeconds: windowSec,
		}
	}
	return nil
}

// evaluateDiskLatencyHigh checks if disk I/O latency exceeds threshold.
func evaluateDiskLatencyHigh(vm query.VMItem, th Thresholds, windowSec float64) *Finding {
	if vm.DiskLatencyMs == nil {
		return nil
	}

	lat := vm.DiskLatencyMs.Avg
	if lat >= th.DiskLatencyMs {
		sev := SeverityWarning
		if lat >= 100.0 {
			sev = SeverityCritical
		}

		return &Finding{
			ID:       RuleDiskLatencyHigh,
			Severity: sev,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("VM %s/%s disk latency is high at %.1fms (threshold: %.0fms)",
				vm.Namespace, vm.Name, lat, th.DiskLatencyMs),
			Evidence: map[string]interface{}{
				"latency_ms_avg":  lat,
				"latency_ms_last": vm.DiskLatencyMs.Last,
				"threshold_ms":    th.DiskLatencyMs,
			},
			WindowSeconds: windowSec,
		}
	}
	return nil
}

// evaluateMigrationNotConverging checks if a live migration is failing to converge.
func evaluateMigrationNotConverging(vm query.VMItem, windowSec float64) *Finding {
	if !vm.IsMigrating || vm.Migration == nil {
		return nil
	}

	// Check if migration is marked in non-progressing condition
	if vm.Migration.Phase == "Failed" || (vm.Migration.Phase == "Running" && vm.Stale) {
		return &Finding{
			ID:       RuleMigrationNotConverging,
			Severity: SeverityWarning,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("Live migration of VM %s/%s from %s to %s is not progressing (phase: %s)",
				vm.Namespace, vm.Name, vm.Migration.SourceNode, vm.Migration.TargetNode, vm.Migration.Phase),
			Evidence: map[string]interface{}{
				"source_node": vm.Migration.SourceNode,
				"target_node": vm.Migration.TargetNode,
				"phase":       vm.Migration.Phase,
			},
			WindowSeconds: windowSec,
		}
	}
	return nil
}

// evaluateMetricsMissing checks for missing balloon driver or stale scrapes.
func evaluateMetricsMissing(vm query.VMItem, th Thresholds, windowSec float64) []Finding {
	var findings []Finding

	// Case A: Running VM without balloon stats
	if (vm.Phase == "Running" || vm.Phase == "") && vm.MemGuestUsedBytes == nil {
		findings = append(findings, Finding{
			ID:       RuleMetricsMissing,
			Severity: SeverityInfo,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("VM %s/%s is running without guest balloon memory statistics",
				vm.Namespace, vm.Name),
			Evidence: map[string]interface{}{
				"reason": "no balloon stats reported",
				"phase":  vm.Phase,
			},
			WindowSeconds: windowSec,
		})
	}

	// Case B: Stale scrape
	if (vm.Phase == "Running" || vm.Phase == "") && vm.Stale && vm.StaleAgeSeconds >= th.StaleThresholdSec {
		findings = append(findings, Finding{
			ID:       RuleMetricsMissing,
			Severity: SeverityWarning,
			Subject: Subject{
				Kind:      SubjectKindVM,
				Name:      vm.Name,
				Namespace: vm.Namespace,
			},
			Summary: fmt.Sprintf("VM %s/%s metrics are stale (last seen %.0fs ago)",
				vm.Namespace, vm.Name, vm.StaleAgeSeconds),
			Evidence: map[string]interface{}{
				"reason":                "stale metrics",
				"last_seen_seconds_ago": vm.StaleAgeSeconds,
				"threshold_seconds":     th.StaleThresholdSec,
			},
			WindowSeconds: windowSec,
		})
	}

	return findings
}

// evaluateNodeOvercommit checks if a node's VM allocated memory exceeds allocatable threshold.
func evaluateNodeOvercommit(node query.NodeItem, th Thresholds, windowSec float64) *Finding {
	if node.NodeAllocatableBytes <= 0 {
		return nil
	}

	if node.OvercommitRatio >= th.NodeOvercommitRatio {
		sev := SeverityWarning
		if node.OvercommitRatio >= 2.0 {
			sev = SeverityCritical
		}

		return &Finding{
			ID:       RuleNodeOvercommit,
			Severity: sev,
			Subject: Subject{
				Kind: SubjectKindNode,
				Name: node.NodeName,
			},
			Summary: fmt.Sprintf("Node %s memory is overcommitted at %.1f%% of physical allocatable memory (threshold: %.0f%%)",
				node.NodeName, node.OvercommitRatio*100, th.NodeOvercommitRatio*100),
			Evidence: map[string]interface{}{
				"memory_allocated_bytes": node.MemAllocatedBytes,
				"node_allocatable_bytes": node.NodeAllocatableBytes,
				"overcommit_ratio":       node.OvercommitRatio,
				"threshold_ratio":        th.NodeOvercommitRatio,
			},
			WindowSeconds: windowSec,
		}
	}
	return nil
}

// evaluateNodeImbalance checks if any node's VM load significantly exceeds the cluster median.
func evaluateNodeImbalance(nodes []query.NodeItem, allVMs []query.VMItem, th Thresholds, windowSec float64) []Finding {
	if len(nodes) < 2 {
		return nil
	}

	var cpuValues []float64
	for _, n := range nodes {
		cpuValues = append(cpuValues, n.CPUCoresUsed)
	}
	medianCPU := computeMedian(cpuValues)

	// Avoid triggering if cluster load is negligible (< 0.1 cores median)
	if medianCPU < 0.1 {
		return nil
	}

	var findings []Finding
	for _, n := range nodes {
		ratio := n.CPUCoresUsed / medianCPU
		if ratio >= th.NodeImbalanceRatio {
			// Find top contributing VMs on this node
			var nodeVMs []query.VMItem
			for _, vm := range allVMs {
				if vm.Node == n.NodeName {
					nodeVMs = append(nodeVMs, vm)
				}
			}
			sort.Slice(nodeVMs, func(i, j int) bool {
				cpuI := 0.0
				if nodeVMs[i].CPUCoresUsed != nil {
					cpuI = nodeVMs[i].CPUCoresUsed.Avg
				}
				cpuJ := 0.0
				if nodeVMs[j].CPUCoresUsed != nil {
					cpuJ = nodeVMs[j].CPUCoresUsed.Avg
				}
				return cpuI > cpuJ
			})

			var topContributors []string
			for i := 0; i < len(nodeVMs) && i < 3; i++ {
				c := 0.0
				if nodeVMs[i].CPUCoresUsed != nil {
					c = nodeVMs[i].CPUCoresUsed.Avg
				}
				topContributors = append(topContributors, fmt.Sprintf("%s/%s (%.2fc)", nodeVMs[i].Namespace, nodeVMs[i].Name, c))
			}

			findings = append(findings, Finding{
				ID:       RuleNodeImbalance,
				Severity: SeverityWarning,
				Subject: Subject{
					Kind: SubjectKindNode,
					Name: n.NodeName,
				},
				Summary: fmt.Sprintf("Node %s CPU load (%.2fc) is %.1fx the cluster median (%.2fc). Top contributors: %s",
					n.NodeName, n.CPUCoresUsed, ratio, medianCPU, strings.Join(topContributors, ", ")),
				Evidence: map[string]interface{}{
					"node_cpu_cores":   n.CPUCoresUsed,
					"median_cpu_cores": medianCPU,
					"imbalance_ratio":  ratio,
					"threshold_ratio":  th.NodeImbalanceRatio,
					"top_contributors": topContributors,
				},
				WindowSeconds: windowSec,
			})
		}
	}

	return findings
}

func computeMedian(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2.0
}
