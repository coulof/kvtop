package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/coulof/kvtop/internal/query"
)

// RunNodes handles the `kvtop nodes` subcommand.
func RunNodes(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nodes", flag.ContinueOnError)
	fs.SetOutput(stderr)

	limit := fs.Int("n", 0, "Maximum number of nodes to report (0 for all)")
	fs.IntVar(limit, "limit", 0, "Maximum number of nodes to report (alias for -n)")
	window := fs.Duration("window", 15*time.Second, "Sample window duration (minimum 10s)")
	outputFormat := fs.String("o", "json", "Output format: json, table")

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

	var progressOut io.Writer
	if *outputFormat == "table" {
		progressOut = stderr
	}

	if err := WarmUpWindow(ctx, env, *window, *interval, progressOut); err != nil {
		PrintJSONError(stdout, fmt.Errorf("failed collecting window samples: %w", err))
		return 1
	}

	result, err := env.Engine.QueryNodes(ctx, query.NodesOptions{
		Window: *window,
		Limit:  *limit,
	})
	if err != nil {
		PrintJSONError(stdout, err)
		return 1
	}

	if *outputFormat == "table" {
		PrintNodesTable(stdout, result)
	} else {
		if err := PrintJSON(stdout, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
			return 1
		}
	}

	return 0
}
