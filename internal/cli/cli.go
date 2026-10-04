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
  kvtop top [flags]                            List top VMs by resource usage (CPU, memory, net, disk)
  kvtop vm <namespace>/<name> [flags]          Inspect a specific VM (sizing, conditions, volumes, metrics)
  kvtop nodes [flags]                          List cluster nodes, capacity, VM density, and overcommit
  kvtop diagnose [flags]                       Run deterministic health diagnosis (saturation, latency, imbalance)
  kvtop record --out <dir> [flags]             Record cluster metrics to directory for --replay
  kvtop mcp [flags]                            Start Model Context Protocol (MCP) stdio server
  kvtop help [command]                         Show this help message

Commands:
  top        List top VMs sorted by metric (--sort cpu|mem|net|disk)
  vm         Detailed breakdown of a single VM with attached informer context
  nodes      Physical cluster host overview with allocatable capacity and overcommit
  diagnose   Deterministic rule-based anomaly detection on VMs and nodes
  record     Capture live cluster scrapes and informers for offline replay
  mcp        Expose top, vm, nodes, and diagnose as tools via MCP stdio protocol

Common Flags:
  --window <duration>     Metrics sampling window (default: 15s for top/vm/nodes, 30s for diagnose, min: 10s)
  -o, --output format     Output format: json, table (default: json for subcommands)
  -n, --limit <int>       Limit results (default: 10 for top)
  -q, --quiet             Suppress progress updates on stderr during sampling window
  --replay <dir>          Run against recorded scrape fixtures
  --kubeconfig <path>     Path to kubeconfig file
  --interval <duration>   Scrape polling interval (default: 2s)

Run 'kvtop <command> --help' for command-specific flags and thresholds.`)
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
	case "record":
		return RunRecord(ctx, subArgs, stdout, stderr)
	case "mcp":
		return RunMCP(ctx, subArgs, stdout, stderr)
	case "help", "-h", "--help":
		PrintUsage(stdout)
		return 0
	default:
		PrintJSONError(stdout, fmt.Errorf("unknown subcommand '%s'. Run 'kvtop help' for usage", subcmd))
		return 1
	}
}
