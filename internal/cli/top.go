package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/coulof/kvtop/internal/query"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	parts := strings.Split(val, ",")
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
}

// RunTop handles the `kvtop top` subcommand.
func RunTop(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var nsList stringSliceFlag
	fs.Var(&nsList, "ns", "Filter by namespace (can be repeated or comma-separated)")
	fs.Var(&nsList, "namespace", "Filter by namespace (alias for --ns)")

	sortBy := fs.String("sort", "cpu", "Sort by: cpu, mem, net, disk")
	bySaturation := fs.Bool("by-saturation", false, "Sort CPU by saturation (used/allotted)")
	fs.BoolVar(bySaturation, "saturation", false, "Sort CPU by saturation (alias for --by-saturation)")
	node := fs.String("node", "", "Filter by node name")
	limit := fs.Int("n", 10, "Maximum number of VMs to report")
	fs.IntVar(limit, "limit", 10, "Maximum number of VMs to report (alias for -n)")
	window := fs.Duration("window", 15*time.Second, "Sample window duration (minimum 10s)")
	outputFormat := fs.String("o", "json", "Output format: json, table")
	samples := fs.Bool("samples", false, "Include raw time-series samples")

	// Common connection flags
	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig")
	replayDir := fs.String("replay", "", "Directory containing recorded scrape fixtures")
	virtHandlerNS := fs.String("virt-handler-namespace", "harvester-system", "virt-handler namespace")
	interval := fs.Duration("interval", 2*time.Second, "Scrape polling interval")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		PrintJSONError(stdout, err)
		return 1
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

	// Show progress only in table mode or if stderr is a terminal
	var progressOut io.Writer
	if *outputFormat == "table" {
		progressOut = stderr
	}

	if err := WarmUpWindow(ctx, env, *window, *interval, progressOut); err != nil {
		PrintJSONError(stdout, fmt.Errorf("failed collecting window samples: %w", err))
		return 1
	}

	result, err := env.Engine.QueryTop(ctx, query.TopOptions{
		SortBy:         *sortBy,
		BySaturation:   *bySaturation,
		Namespaces:     nsList,
		Node:           *node,
		Limit:          *limit,
		Window:         *window,
		IncludeSamples: *samples,
	})
	if err != nil {
		PrintJSONError(stdout, err)
		return 1
	}

	if *outputFormat == "table" {
		PrintTopTable(stdout, result)
	} else {
		if err := PrintJSON(stdout, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
			return 1
		}
	}

	return 0
}
