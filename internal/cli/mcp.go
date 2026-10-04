package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/mcp"
)

// Version can be populated by main.go build flags.
var Version = "dev"

// RunMCP handles the `kvtop mcp` subcommand, running a Model Context Protocol stdio server.
func RunMCP(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)

	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig")
	replayDir := fs.String("replay", "", "Directory containing recorded scrape fixtures to replay")
	virtHandlerNS := fs.String("virt-handler-namespace", "harvester-system", "virt-handler namespace")
	interval := fs.Duration("interval", 2*time.Second, "Scrape polling interval")
	allowExec := fs.Bool("allow-exec", false, "Enable virsh exec compute container drilldown")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		PrintJSONError(stdout, err)
		return 1
	}

	cfg := CommonConfig{
		Kubeconfig:           *kubeconfig,
		ReplayDir:            *replayDir,
		VirtHandlerNamespace: *virtHandlerNS,
		Interval:             *interval,
	}

	env, err := InitRuntime(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Failed to initialize runtime: %v\n", err)
		return 1
	}
	defer env.Cleanup()

	// Capture initial baseline sample
	if initSamples, err := env.Collector.Collect(ctx); err == nil {
		env.Store.PushSamples(initSamples)
	}

	// In replay mode: seed initial samples to warm up buffers
	if env.IsReplay {
		for i := 0; i < 3; i++ {
			if s, err := env.Collector.Collect(ctx); err == nil {
				env.Store.PushSamples(s)
			}
		}
	}

	// Start background scraping loop to keep ring buffers warm
	go func() {
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				samples, err := env.Collector.Collect(ctx)
				if err == nil {
					env.Store.PushSamples(samples)
				}
			}
		}
	}()

	diagnoser := diagnose.NewDiagnoser(env.Engine)
	srv := mcp.NewServer(env.Engine, diagnoser, *allowExec, Version)

	// Run stdio server (reading from os.Stdin, writing to stdout)
	if err := srv.Serve(ctx, os.Stdin, stdout); err != nil && err != io.EOF && ctx.Err() == nil {
		fmt.Fprintf(stderr, "MCP server error: %v\n", err)
		return 1
	}

	return 0
}
