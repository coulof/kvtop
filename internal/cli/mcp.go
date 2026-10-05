package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/mcp"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
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

	st := store.NewStore(300)
	engine := query.NewEngine(st, nil, *replayDir)
	diagnoser := diagnose.NewDiagnoser(engine)
	srv := mcp.NewServer(engine, diagnoser, *allowExec, Version)

	if *replayDir != "" {
		// Replay mode: read local fixtures synchronously (takes ~2ms) so tools have warm data immediately
		collector, err := collect.NewReplayCollector(*replayDir, *interval)
		if err != nil {
			engine.SetClusterError(err)
			fmt.Fprintf(stderr, "[kvtop] Warning: replay init error: %v\n", err)
		} else {
			if vmis, err := collect.LoadReplayVMIs(*replayDir); err == nil && len(vmis) > 0 {
				st.UpdateVMIs(vmis)
			}
			for _, n := range []string{"hv-01", "hv-02", "hv-03", "hv-04"} {
				st.OnNodeUpdated(kube.NodeInfo{
					Name:            n,
					AllocatableMem:  96 * 1024 * 1024 * 1024,
					AllocatableCPUs: 16,
					Ready:           true,
				})
			}
			for i := 0; i < 3; i++ {
				if s, err := collector.Collect(ctx); err == nil {
					st.PushSamples(s)
				}
			}
			go func() {
				ticker := time.NewTicker(*interval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if s, err := collector.Collect(ctx); err == nil {
							st.PushSamples(s)
						}
					}
				}
			}()
		}
	} else {
		// Live cluster mode: run connection, informers, and scraping asynchronously in background
		// so the stdio JSON-RPC handshake (initialize, tools/list) answers immediately.
		go func() {
			kClient, err := kube.NewClient(*kubeconfig)
			if err != nil {
				engine.SetClusterError(err)
				fmt.Fprintf(stderr, "[kvtop] Warning: cluster client init error: %v\n", err)
				return
			}

			collector := collect.NewVirtHandlerCollector(kClient, *virtHandlerNS, *interval-300*time.Millisecond)

			// Start Informers
			informerMgr := kube.NewInformerManager(kClient, st, 10*time.Minute)
			go func() {
				if err := informerMgr.Start(ctx); err != nil {
					engine.SetClusterError(err)
					fmt.Fprintf(stderr, "[kvtop] Warning: informer sync: %v\n", err)
				}
			}()
			defer informerMgr.Stop()

			// Fetch initial VMIs with timeout
			listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
			if vmis, err := kClient.ListVMIs(listCtx); err == nil {
				st.UpdateVMIs(vmis)
			} else {
				engine.SetClusterError(err)
				fmt.Fprintf(stderr, "[kvtop] Warning: initial VMI listing failed: %v\n", err)
			}
			listCancel()

			// Initial baseline sample with timeout
			scrapeCtx, scrapeCancel := context.WithTimeout(ctx, 5*time.Second)
			if initSamples, err := collector.Collect(scrapeCtx); err == nil {
				st.PushSamples(initSamples)
			}
			scrapeCancel()

			// Background collection loop
			ticker := time.NewTicker(*interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if samples, err := collector.Collect(ctx); err == nil {
						st.PushSamples(samples)
					}
				}
			}
		}()
	}

	// Serve stdio immediately on main thread
	if err := srv.Serve(ctx, os.Stdin, stdout); err != nil && err != io.EOF && ctx.Err() == nil {
		fmt.Fprintf(stderr, "MCP server error: %v\n", err)
		return 1
	}

	return 0
}
