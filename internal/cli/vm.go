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

// RunVM handles the `kvtop vm <namespace>/<name>` subcommand.
func RunVM(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vm", flag.ContinueOnError)
	fs.SetOutput(stderr)

	window := fs.Duration("window", 15*time.Second, "Sample window duration (minimum 10s)")
	outputFormat := fs.String("o", "json", "Output format: json, table")
	samples := fs.Bool("samples", false, "Include raw time-series samples")
	allowExec := fs.Bool("allow-exec", false, "Allow virsh exec drilldown into launcher pod")
	quiet := fs.Bool("q", false, "Suppress progress output on stderr")
	fs.BoolVar(quiet, "quiet", false, "Suppress progress output on stderr (alias for -q)")

	// Common connection flags
	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig")
	replayDir := fs.String("replay", "", "Directory containing recorded scrape fixtures")
	virtHandlerNS := fs.String("virt-handler-namespace", "harvester-system", "virt-handler namespace")
	interval := fs.Duration("interval", 2*time.Second, "Scrape polling interval")

	// Allow `kvtop vm <target> [flags]` as well as `kvtop vm [flags] <target>`
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

	if target == "" {
		PrintJSONError(stdout, fmt.Errorf("missing required VM argument <namespace>/<name>"))
		return 1
	}

	parts := strings.Split(target, "/")
	var ns, name string
	if len(parts) == 2 {
		ns = parts[0]
		name = parts[1]
	} else if len(parts) == 1 {
		ns = "default"
		name = parts[0]
	} else {
		PrintJSONError(stdout, fmt.Errorf("invalid VM target format '%s' (expected <namespace>/<name>)", target))
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
	if !*quiet {
		progressOut = stderr
	}

	if err := WarmUpWindow(ctx, env, *window, *interval, progressOut); err != nil {
		PrintJSONError(stdout, fmt.Errorf("failed collecting window samples: %w", err))
		return 1
	}

	result, err := env.Engine.QueryVM(ctx, ns, name, query.VMOptions{
		Window:         *window,
		IncludeSamples: *samples,
		AllowExec:      *allowExec,
	})
	if err != nil {
		PrintJSONError(stdout, err)
		return 1
	}

	if *outputFormat == "table" {
		PrintVMTable(stdout, result)
	} else {
		if err := PrintJSON(stdout, result); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing JSON: %v\n", err)
			return 1
		}
	}

	return 0
}
