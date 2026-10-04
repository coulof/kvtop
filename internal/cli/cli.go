package cli

import (
	"context"
	"fmt"
	"io"
)

// PrintUsage prints the CLI help and available subcommands.
func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, `kvtop: live troubleshooting for KubeVirt VMs on Harvester

Usage:
  kvtop [flags]                                Launch interactive TUI
  kvtop top [flags]                            List top VMs by resource usage
  kvtop vm <namespace>/<name> [flags]          Inspect a specific VM
  kvtop nodes [flags]                          List cluster nodes and capacity
  kvtop diagnose [flags]                       Run deterministic health diagnosis
  kvtop help                                   Show this help message

Subcommand Flags:
  --window <duration>     Metrics sampling window (default 15s, min 10s)
  -o json|table           Output format (default: json for subcommands)
  --replay <dir>          Run against recorded scrape fixtures
  --kubeconfig <path>     Path to kubeconfig file

Run 'kvtop <command> --help' for command-specific flags.`)
}

// Run routes and executes non-interactive kvtop subcommands.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		PrintUsage(stderr)
		return 1
	}

	subcmd := args[0]
	subArgs := args[1:]

	switch subcmd {
	case "top":
		return RunTop(ctx, subArgs, stdout, stderr)
	case "vm":
		return RunVM(ctx, subArgs, stdout, stderr)
	case "nodes":
		return RunNodes(ctx, subArgs, stdout, stderr)
	case "diagnose":
		return RunDiagnose(ctx, subArgs, stdout, stderr)
	case "help", "-h", "--help":
		PrintUsage(stdout)
		return 0
	default:
		PrintJSONError(stdout, fmt.Errorf("unknown subcommand '%s'. Run 'kvtop help' for usage", subcmd))
		return 1
	}
}
