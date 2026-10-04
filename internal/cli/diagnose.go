package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/query"
)

// RunDiagnose handles the `kvtop diagnose` subcommand.
func RunDiagnose(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var nsList stringSliceFlag
	fs.Var(&nsList, "ns", "Filter by namespace (can be repeated or comma-separated)")
	fs.Var(&nsList, "namespace", "Filter by namespace (alias for --ns)")

	node := fs.String("node", "", "Filter by node name")
	window := fs.Duration("window", 30*time.Second, "Sample window duration (minimum 10s)")
	outputFormat := fs.String("o", "json", "Output format: json, table")
	quiet := fs.Bool("q", false, "Suppress progress output on stderr")
	fs.BoolVar(quiet, "quiet", false, "Suppress progress output on stderr (alias for -q)")

	// Configurable thresholds
	defTh := diagnose.DefaultThresholds()
	thCPU := fs.Float64("threshold-cpu", defTh.CPUSaturationRatio, "CPU saturation ratio threshold (0.0 - 1.0)")
	thMem := fs.Float64("threshold-mem", defTh.MemGuestPressureRatio, "Guest memory pressure ratio threshold (0.0 - 1.0)")
	thLatency := fs.Float64("threshold-latency", defTh.DiskLatencyMs, "Disk I/O latency threshold in ms")
	thOvercommit := fs.Float64("threshold-overcommit", defTh.NodeOvercommitRatio, "Node memory overcommit ratio threshold")
	thImbalance := fs.Float64("threshold-imbalance", defTh.NodeImbalanceRatio, "Node imbalance ratio relative to cluster median")
	thStale := fs.Float64("threshold-stale", defTh.StaleThresholdSec, "Stale metrics timeout in seconds")

	// Common connection flags
	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig")
	replayDir := fs.String("replay", "", "Directory containing recorded scrape fixtures")
	virtHandlerNS := fs.String("virt-handler-namespace", "harvester-system", "virt-handler namespace")
	interval := fs.Duration("interval", 2*time.Second, "Scrape polling interval")

	// Allow positional target VM: `kvtop diagnose default/my-vm [flags]` or `kvtop diagnose [flags] default/my-vm`
	var target string
	var flagArgs []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		flagArgs = args[1:]
	} else {
		flagArgs = args
	}

	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		PrintJSONError(stdout, err)
		return 1
	}

	if target == "" {
		remaining := fs.Args()
		if len(remaining) > 0 {
			target = remaining[0]
		}
	}

	if *window < 10*time.Second {
		PrintJSONError(stdout, query.ErrWindowTooSmall)
		return 1
	}

	cfg := CommonConfig{
		Kubeconfig:           *kubeconfig,
		ReplayDir:            *replayDir,
		VirtHandlerNamespace: *virtHandlerNS,
		Interval:             *interval,
		Window:               *window,
		OutputFormat:         *outputFormat,
	}

	env, err := InitRuntime(ctx, cfg)
	if err != nil {
		PrintJSONError(stdout, err)
		return 1
	}
	defer env.Cleanup()

	var progressOut io.Writer
	if !*quiet {
		progressOut = stderr
	}

	if err := WarmUpWindow(ctx, env, *window, *interval, progressOut); err != nil {
		PrintJSONError(stdout, fmt.Errorf("failed collecting window samples: %w", err))
		return 1
	}

	d := diagnose.NewDiagnoser(env.Engine)
	result, err := d.Diagnose(ctx, diagnose.DiagnoseOptions{
		TargetVM:   target,
		Namespaces: nsList,
		Node:       *node,
		Window:     *window,
		Thresholds: diagnose.Thresholds{
			CPUSaturationRatio:    *thCPU,
			MemGuestPressureRatio: *thMem,
			DiskLatencyMs:         *thLatency,
			NodeOvercommitRatio:   *thOvercommit,
			NodeImbalanceRatio:    *thImbalance,
			StaleThresholdSec:     *thStale,
		},
	})
	if err != nil {
		PrintJSONError(stdout, err)
		return 1
	}

	if *outputFormat == "table" {
		PrintDiagnoseTable(stdout, result)
	} else {
		if err := PrintJSON(stdout, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
			return 1
		}
	}

	return 0
}
