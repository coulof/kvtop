package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
)

// CommonConfig contains runtime flags shared across CLI commands.
type CommonConfig struct {
	Kubeconfig           string
	ReplayDir            string
	VirtHandlerNamespace string
	Interval             time.Duration
	Window               time.Duration
	OutputFormat         string // "json" or "table"
}

// RuntimeEnvironment bundles initialized store, engine, collector, and teardown func.
type RuntimeEnvironment struct {
	Store     *store.Store
	Engine    *query.Engine
	Collector collect.Collector
	IsReplay  bool
	Cleanup   func()
}

// InitRuntime sets up store, replay/kubernetes informers, and query engine.
func InitRuntime(ctx context.Context, cfg CommonConfig) (*RuntimeEnvironment, error) {
	st := store.NewStore(300)
	var (
		collector collect.Collector
		kClient   *kube.Client
		cleanup   = func() {}
		isReplay  = cfg.ReplayDir != ""
		err       error
	)

	if isReplay {
		collector, err = collect.NewReplayCollector(cfg.ReplayDir, cfg.Interval)
		if err != nil {
			return nil, fmt.Errorf("failed to init replay collector: %w", err)
		}

		if vmis, err := collect.LoadReplayVMIs(cfg.ReplayDir); err == nil && len(vmis) > 0 {
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
		kClient, err = kube.NewClient(cfg.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to init kubernetes client: %w", err)
		}

		collector = collect.NewVirtHandlerCollector(kClient, cfg.VirtHandlerNamespace, cfg.Interval-300*time.Millisecond)

		// Start Informers
		informerMgr := kube.NewInformerManager(kClient, st, 10*time.Minute)
		go func() {
			_ = informerMgr.Start(ctx)
		}()
		cleanup = func() {
			informerMgr.Stop()
		}

		// Initial VMI list
		if vmis, err := kClient.ListVMIs(ctx); err == nil {
			st.UpdateVMIs(vmis)
		}
	}

	engine := query.NewEngine(st, kClient, cfg.ReplayDir)

	return &RuntimeEnvironment{
		Store:     st,
		Engine:    engine,
		Collector: collector,
		IsReplay:  isReplay,
		Cleanup:   cleanup,
	}, nil
}

// WarmUpWindow collects samples across the requested window to compute valid rates.
func WarmUpWindow(ctx context.Context, env *RuntimeEnvironment, window, interval time.Duration, progressOut io.Writer) error {
	if window < 10*time.Second {
		return query.ErrWindowTooSmall
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}

	// 1. Initial baseline sample
	initSamples, err := env.Collector.Collect(ctx)
	if err == nil {
		env.Store.PushSamples(initSamples)
	}

	// 2. Replay mode: fast-forward simulated ticks instantaneously
	if env.IsReplay {
		ticks := int(window / interval)
		if ticks < 2 {
			ticks = 2
		}
		for i := 0; i < ticks; i++ {
			samples, err := env.Collector.Collect(ctx)
			if err != nil {
				return err
			}
			env.Store.PushSamples(samples)
		}
		return nil
	}

	// 3. Live cluster mode: block across the window
	if progressOut != nil {
		fmt.Fprintf(progressOut, "Waiting for %v metrics collection window to compute rates...\n", window)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	target := time.Now().Add(window)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			samples, err := env.Collector.Collect(ctx)
			if err == nil {
				env.Store.PushSamples(samples)
			}
			if time.Now().After(target) {
				return nil
			}
		}
	}
}
