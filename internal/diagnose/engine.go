package diagnose

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/coulof/kvtop/internal/query"
)

// Diagnoser coordinates the deterministic rule evaluation over query.Engine.
type Diagnoser struct {
	queryEngine *query.Engine
}

// NewDiagnoser creates a new diagnosis engine.
func NewDiagnoser(engine *query.Engine) *Diagnoser {
	return &Diagnoser{
		queryEngine: engine,
	}
}

// Diagnose executes the initial deterministic rule set and returns findings.
func (d *Diagnoser) Diagnose(ctx context.Context, opts DiagnoseOptions) (*DiagnoseResult, error) {
	if opts.Window <= 0 {
		opts.Window = 30 * time.Second
	}
	if opts.Window < 10*time.Second {
		return nil, query.ErrWindowTooSmall
	}

	th := opts.Thresholds
	def := DefaultThresholds()
	if th.CPUSaturationRatio <= 0 {
		th.CPUSaturationRatio = def.CPUSaturationRatio
	}
	if th.MemGuestPressureRatio <= 0 {
		th.MemGuestPressureRatio = def.MemGuestPressureRatio
	}
	if th.DiskLatencyMs <= 0 {
		th.DiskLatencyMs = def.DiskLatencyMs
	}
	if th.NodeOvercommitRatio <= 0 {
		th.NodeOvercommitRatio = def.NodeOvercommitRatio
	}
	if th.NodeImbalanceRatio <= 0 {
		th.NodeImbalanceRatio = def.NodeImbalanceRatio
	}
	if th.StaleThresholdSec <= 0 {
		th.StaleThresholdSec = def.StaleThresholdSec
	}

	now := time.Now()
	windowSec := opts.Window.Seconds()
	var findings []Finding

	// 1. Diagnose specific target VM if specified
	if opts.TargetVM != "" {
		parts := strings.Split(opts.TargetVM, "/")
		var ns, name string
		if len(parts) == 2 {
			ns = parts[0]
			name = parts[1]
		} else {
			ns = "default"
			name = parts[0]
		}

		vmRes, err := d.queryEngine.QueryVM(ctx, ns, name, query.VMOptions{
			Window: opts.Window,
		})
		if err != nil {
			return nil, err
		}

		findings = append(findings, d.evaluateVMRules(vmRes.VM, th, windowSec)...)

		d.sortFindings(findings)
		return &DiagnoseResult{
			Schema:        query.SchemaVersion,
			CollectedAt:   now.UTC().Format(time.RFC3339),
			WindowSeconds: windowSec,
			TotalFindings: len(findings),
			Findings:      findings,
		}, nil
	}

	// 2. Multi-VM / Cluster-wide diagnosis
	topRes, err := d.queryEngine.QueryTop(ctx, query.TopOptions{
		Namespaces: opts.Namespaces,
		Node:       opts.Node,
		Limit:      10000,
		Window:     opts.Window,
	})
	if err != nil {
		return nil, fmt.Errorf("failed querying VMs for diagnosis: %w", err)
	}

	for _, vm := range topRes.Items {
		findings = append(findings, d.evaluateVMRules(vm, th, windowSec)...)
	}

	// 3. Node-level rules
	nodesRes, err := d.queryEngine.QueryNodes(ctx, query.NodesOptions{
		Window: opts.Window,
	})
	if err == nil {
		for _, n := range nodesRes.Items {
			if opts.Node != "" && n.NodeName != opts.Node {
				continue
			}
			if f := evaluateNodeOvercommit(n, th, windowSec); f != nil {
				findings = append(findings, *f)
			}
		}

		// Node imbalance rule: only evaluate when examining cluster-wide (no node filter)
		if opts.Node == "" && len(opts.Namespaces) == 0 {
			imbalanceFindings := evaluateNodeImbalance(nodesRes.Items, topRes.Items, th, windowSec)
			findings = append(findings, imbalanceFindings...)
		}
	}

	d.sortFindings(findings)

	return &DiagnoseResult{
		Schema:        query.SchemaVersion,
		CollectedAt:   now.UTC().Format(time.RFC3339),
		WindowSeconds: windowSec,
		TotalFindings: len(findings),
		Findings:      findings,
	}, nil
}

func (d *Diagnoser) evaluateVMRules(vm query.VMItem, th Thresholds, windowSec float64) []Finding {
	var list []Finding

	if f := evaluateCPUSaturated(vm, th, windowSec); f != nil {
		list = append(list, *f)
	}
	if f := evaluateMemGuestPressure(vm, th, windowSec); f != nil {
		list = append(list, *f)
	}
	if f := evaluateDiskLatencyHigh(vm, th, windowSec); f != nil {
		list = append(list, *f)
	}
	if f := evaluateMigrationNotConverging(vm, windowSec); f != nil {
		list = append(list, *f)
	}
	list = append(list, evaluateMetricsMissing(vm, th, windowSec)...)

	return list
}

func (d *Diagnoser) sortFindings(findings []Finding) {
	severityWeight := func(sev string) int {
		switch sev {
		case SeverityCritical:
			return 3
		case SeverityWarning:
			return 2
		case SeverityInfo:
			return 1
		default:
			return 0
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		wI := severityWeight(findings[i].Severity)
		wJ := severityWeight(findings[j].Severity)
		if wI != wJ {
			return wI > wJ // higher severity first
		}
		if findings[i].ID != findings[j].ID {
			return findings[i].ID < findings[j].ID
		}
		if findings[i].Subject.Namespace != findings[j].Subject.Namespace {
			return findings[i].Subject.Namespace < findings[j].Subject.Namespace
		}
		return findings[i].Subject.Name < findings[j].Subject.Name
	})
}
