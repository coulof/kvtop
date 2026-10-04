package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coulof/kvtop/internal/cli"
	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui"
)

var (
	// Version is populated at build time via -ldflags.
	Version = "dev"
	// GitCommit is populated at build time via -ldflags.
	GitCommit = "none"
	// BuildDate is populated at build time via -ldflags.
	BuildDate = "unknown"
)

func main() {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
		os.Exit(code)
	}

	showVersion := flag.Bool("version", false, "Print version information and exit")
	flag.BoolVar(showVersion, "v", false, "Print version information and exit")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file")
	replayDir := flag.String("replay", "", "Directory containing recorded scrape fixtures to replay")
	namespace := flag.String("namespace", "", "Filter by namespace (defaults to all)")
	virtHandlerNS := flag.String("virt-handler-namespace", "harvester-system", "Namespace where virt-handler pods run")
	interval := flag.Duration("interval", 2*time.Second, "Scrape interval")
	plainText := flag.Bool("plain-text", false, "Output plain text table instead of interactive TUI")
	count := flag.Int("count", 0, "Number of table updates to print (only in plain-text mode, 0 for continuous)")
	sortBy := flag.String("sort", "name", "Sort by: name, cpu, mem, net, disk")
	bySaturation := flag.Bool("by-saturation", false, "Sort CPU by saturation (used/allotted) instead of absolute cores")
	flag.Parse()

	if *showVersion {
		fmt.Printf("kvtop %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate)
		os.Exit(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	st := store.NewStore(300)

	var (
		collector   collect.Collector
		kClient     *kube.Client
		clusterName = "harvester"
		kubeVersion = "v1.36.3"
		err         error
	)

	if *replayDir != "" {
		clusterName = "replay"
		kubeVersion = "offline"
		collector, err = collect.NewReplayCollector(*replayDir, *interval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing replay collector: %v\n", err)
			os.Exit(1)
		}

		if vmis, err := collect.LoadReplayVMIs(*replayDir); err == nil && len(vmis) > 0 {
			st.UpdateVMIs(vmis)
		}

		// Seed replay nodes
		for _, n := range []string{"hv-01", "hv-02", "hv-03", "hv-04"} {
			st.OnNodeUpdated(kube.NodeInfo{
				Name:            n,
				AllocatableMem:  96 * 1024 * 1024 * 1024,
				AllocatableCPUs: 16,
				Ready:           true,
			})
		}
	} else {
		kClient, err = kube.NewClient(*kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing Kubernetes client: %v\n", err)
			os.Exit(1)
		}

		// Discover server version
		if versionInfo, err := kClient.Clientset.Discovery().ServerVersion(); err == nil {
			kubeVersion = versionInfo.GitVersion
		}

		collector = collect.NewVirtHandlerCollector(kClient, *virtHandlerNS, *interval-300*time.Millisecond)

		// Start Informers to watch VMI, VMIM, Node, and Namespace in real-time
		informerMgr := kube.NewInformerManager(kClient, st, 10*time.Minute)
		go func() {
			if err := informerMgr.Start(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: informer sync: %v\n", err)
			}
		}()
		defer informerMgr.Stop()

		// Initial VMI list
		if vmis, err := kClient.ListVMIs(ctx); err == nil {
			st.UpdateVMIs(vmis)
		}
	}

	// Capture initial baseline metrics before starting UI
	if initSamples, err := collector.Collect(ctx); err == nil {
		st.PushSamples(initSamples)
	}

	// Start background collection loop
	go func() {
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				samples, err := collector.Collect(ctx)
				if err == nil {
					st.PushSamples(samples)
				}
			}
		}
	}()

	engine := query.NewEngine(st, kClient, *replayDir)

	// If plain text mode is requested or non-interactive count > 0:
	if *plainText || *count > 0 {
		runPlainTextMode(ctx, engine, *namespace, *sortBy, *bySaturation, *interval, *count)
		return
	}

	// Interactive TUI Mode
	app := ui.NewAppModel(engine, clusterName, kubeVersion, *interval)

	if *replayDir != "" {
		replayPath := *replayDir
		app.SetDetailStreamStarter(func(ctx context.Context, namespace, vmiName string, statsChan chan<- collect.VirshStats) (string, error) {
			podName := fmt.Sprintf("virt-launcher-%s-replay", vmiName)
			baseStats, _ := collect.LoadReplayVirshStats(replayPath)
			go func() {
				// Immediate initial tick
				statsChan <- baseStats
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						current := baseStats
						current.Timestamp = time.Now()
						statsChan <- current
					}
				}
			}()
			return podName, nil
		})
	} else if kClient != nil {
		app.SetDetailStreamStarter(func(ctx context.Context, namespace, vmiName string, statsChan chan<- collect.VirshStats) (string, error) {
			podName, err := kClient.FindLauncherPod(ctx, namespace, vmiName)
			if err != nil {
				return "", err
			}
			stream, err := kClient.StreamVirshDomstatsExec(ctx, namespace, podName)
			if err != nil {
				return podName, err
			}
			go func() {
				defer stream.Close()
				collect.StreamVirshDomstats(stream, statsChan, ctx.Done())
			}()
			return podName, nil
		})
	}

	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}
}

func runPlainTextMode(
	ctx context.Context,
	engine query.SnapshotProvider,
	nsFilter string,
	sortBy string,
	bySaturation bool,
	interval time.Duration,
	count int,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	iterations := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := engine.Snapshot(nsFilter)
			sortVMs(snap.VMs, sortBy, bySaturation)
			printSummaryAndTable(snap)

			iterations++
			if count > 0 && iterations >= count {
				return
			}
		}
	}
}

func sortVMs(vms []store.VMSnapshot, sortBy string, bySaturation bool) {
	sort.Slice(vms, func(i, j int) bool {
		switch sortBy {
		case "name":
			if vms[i].Namespace != vms[j].Namespace {
				return vms[i].Namespace < vms[j].Namespace
			}
			return strings.ToLower(vms[i].Name) < strings.ToLower(vms[j].Name)
		case "cpu":
			if bySaturation {
				return vms[i].CPUSaturationPercent > vms[j].CPUSaturationPercent
			}
			return vms[i].CPUUsageCores > vms[j].CPUUsageCores
		case "mem":
			if vms[i].HasBalloonStats != vms[j].HasBalloonStats {
				return vms[i].HasBalloonStats
			}
			return vms[i].MemoryUsedPercent > vms[j].MemoryUsedPercent
		case "net":
			return (vms[i].NetRxBytesPerSec + vms[i].NetTxBytesPerSec) > (vms[j].NetRxBytesPerSec + vms[j].NetTxBytesPerSec)
		case "disk":
			return vms[i].StorageTotalIOPS > vms[j].StorageTotalIOPS
		default:
			if vms[i].Namespace != vms[j].Namespace {
				return vms[i].Namespace < vms[j].Namespace
			}
			return strings.ToLower(vms[i].Name) < strings.ToLower(vms[j].Name)
		}
	})
}

func printSummaryAndTable(snap store.StoreSnapshot) {
	fmt.Println()
	cl := snap.ClusterTotals
	fmt.Printf("CLUSTER | VMs: %d running / %d total | CPU: %.2f cores (%d vCPUs) | Guest Mem: %s | Overcommit: %.1f%%\n",
		cl.RunningVMs,
		cl.TotalVMs,
		cl.CPUUsageCores,
		cl.AllottedCPUs,
		ui.FormatBytes(float64(cl.MemoryUsedBytes)),
		cl.OvercommitRatio*100,
	)

	fmt.Print("NODES   | ")
	for i, n := range snap.Nodes {
		fmt.Printf("[%s: %d VMs, %.2fc, oc:%.0f%%, ▼%s ▲%s, %.1f io]",
			n.NodeName,
			n.VMCount,
			n.CPUUsageCores,
			n.OvercommitRatio*100,
			ui.FormatRate(n.NetRxBytesPerSec),
			ui.FormatRate(n.NetTxBytesPerSec),
			n.StorageTotalIOPS,
		)
		if i < len(snap.Nodes)-1 {
			fmt.Print("  ")
		}
	}
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAMESPACE\tNAME\tIP\tNODE\tCPU (USED/ALLOT)\tCPU SAT%\tMEM GUEST%\tRX\tTX\tIOPS")
	fmt.Fprintln(w, "---------\t----\t--\t----\t----------------\t--------\t----------\t--\t--\t----")

	for _, vm := range snap.VMs {
		ipStr := vm.IP
		if ipStr == "" {
			ipStr = "–"
		}
		cpuStr := fmt.Sprintf("%.2f/%d", vm.CPUUsageCores, vm.AllottedCPUs)
		cpuSatStr := fmt.Sprintf("%.1f%%", vm.CPUSaturationPercent)

		memStr := "–"
		if vm.HasBalloonStats {
			memStr = fmt.Sprintf("%.1f%% (%s/%s)",
				vm.MemoryUsedPercent,
				ui.FormatBytes(float64(vm.MemoryUsedBytes)),
				ui.FormatBytes(float64(vm.MemoryTotalBytes)),
			)
		}

		rxStr := ui.FormatRate(vm.NetRxBytesPerSec)
		txStr := ui.FormatRate(vm.NetTxBytesPerSec)
		iopsStr := fmt.Sprintf("%.1f", vm.StorageTotalIOPS)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			vm.Namespace,
			vm.Name,
			ipStr,
			vm.Node,
			cpuStr,
			cpuSatStr,
			memStr,
			rxStr,
			txStr,
			iopsStr,
		)
	}
	w.Flush()
}
