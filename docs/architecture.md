# kvtop Architecture

`kvtop` (KubeVirt top) is a terminal UI and agent interface for live troubleshooting of Virtual Machines on Harvester and KubeVirt clusters.

This document describes the internal system architecture, data flow, package boundaries, and design principles.

---

## 1. System Overview

`kvtop` is built on a "two frontends, one core" architecture:
- **Human Frontend**: An interactive, responsive terminal user interface (TUI) powered by Bubbletea and Lipgloss, featuring custom 2D Unicode Braille charts and sparklines.
- **Agent Frontend**: A structured, non-interactive JSON CLI and Model Context Protocol (MCP) server providing deterministic metrics, joined context, and diagnosis to AI agents.

Both frontends share the identical read API (`internal/query`) over in-memory ring buffers and real-time Kubernetes informers.

```
┌────────────────────────────────────────────────────────────────────────┐
│                               cmd/kvtop                                │
│       Routes to CLI subcommands (top/vm/nodes), MCP, or TUI            │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
          ┌─────────────────────────┼─────────────────────────┐
          ▼                         ▼                         ▼
 ┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
 │  internal/cli   │       │  internal/mcp   │       │   internal/ui   │
 │  (JSON / Table) │       │ (stdio server)  │       │ (Bubbletea TUI) │
 └────────┬────────┘       └────────┬────────┘       └────────┬────────┘
          │                         │                         │
          └─────────────────────────┼─────────────────────────┘
                                    ▼
                         ┌─────────────────────┐
                         │   internal/query    │  <-- Single shared read API
                         │ (Engine, Summaries) │
                         └──────────┬──────────┘
                                    │
           ┌────────────────────────┼────────────────────────┐
           ▼                        ▼                        ▼
  ┌─────────────────┐      ┌─────────────────┐      ┌─────────────────┐
  │ internal/store  │      │  internal/kube  │      │internal/diagnose│
  │ (Ring, Rates)   │      │(Informers, Exec)│      │(Rules, Evidence)│
  └────────▲────────┘      └─────────────────┘      └─────────────────┘
           │
  ┌────────┴────────┐
  │internal/collect │
  │(virt-handler,   │
  │ virsh, replay)  │
  └─────────────────┘
```

---

## 2. Core Architectural Principles

1. **Client-Side Only**: Single static binary without CGO (`CGO_ENABLED=0`). No DaemonSets, CRDs, or container images are installed in the cluster.
2. **Prometheus Is Optional**: Functions completely with the `rancher-monitoring` addon disabled (default on Harvester). Prometheus is only used to backfill long-range history when present.
3. **Strictly Read-Only**: Performs no mutative VM actions (no start, stop, or migrate operations).
4. **Frontends Read Through `internal/query`**: No frontend communicates directly with `internal/store` or the Kubernetes API. Metric computation and window summaries remain identical across TUI, CLI, and MCP modes.
5. **Bounded Intrusiveness**: The only intrusive operation is streaming `virsh domstats` via SPDY exec into the `virt-launcher` container. In CLI and MCP modes, it is disabled unless `--allow-exec` is explicitly provided.
6. **Air-Gap Ready**: The only external dependency is an administrative `kubeconfig`. All scraping occurs via the Kubernetes API server pod proxy.

---

## 3. Package Structure & Responsibilities

```
cmd/kvtop/            Entry point, flag parsing, subcommand routing, signal handling
internal/collect/     Metric collector implementations and Prometheus/virsh parsers
internal/store/       In-memory ring buffers, counter rate tracking, and aggregations
internal/kube/        Dynamic client informers (VMI, VMIM, Node, Namespace), pod proxy, SPDY exec
internal/query/       Unified read API: windowing, statistical summaries, schema models
internal/cli/         Non-interactive subcommands (top, vm, nodes) with JSON & table output
internal/diagnose/    Deterministic finding engine (CPU saturation, memory pressure, etc.)
internal/mcp/         Model Context Protocol (MCP) stdio server wrapping query & diagnose
internal/ui/          Bubbletea TUI models (header, nodes, VM table, detail view, help)
internal/ui/chart/    Custom 2D braille graphs and sparkline rendering engine
```

---

## 4. Data Flow & Lifecycle

```
[virt-handler pods] ──(GET pod proxy)──► Collector (2s tick)
                                              │
                                       []VMISample
                                              ▼
[K8s Informers] ───(VMI/VMIM/Node)─────► Metric Store
 (real-time)                                  │
                                         Rates, Ring Buffers
                                              ▼
                                         Query Engine
                                              │
                     ┌────────────────────────┴────────────────────────┐
                     ▼                                                 ▼
             Interactive TUI                                    Agent CLI / MCP
        (Bubbletea SnapshotProvider)                      (JSON Schema `kvtop/v1`)
```

### 1. Ingestion (`internal/collect`)
- **virt-handler Scraper**: Collects `/metrics` concurrently across all physical nodes via the Kubernetes API pod proxy:
  `/api/v1/namespaces/harvester-system/pods/https:<pod>:8443/proxy/metrics`
  Scrapes are performed once per node (never once per VM).
- **Domain Stats Cache Handling**: `virt-handler` caches libvirt domain statistics for approximately 5 seconds. The store tracks elapsed time and holds previous rates across cache hits, decaying to zero only when idle beyond 6 seconds.
- **Replay Collector**: Parses recorded fixtures (`testdata/`) to enable offline development, testing, and continuous integration without a cluster.

### 2. Processing & Storage (`internal/store`)
- **Counter Delta Rates**: Evaluated using client-side wall-clock timestamps:
  `rate = (counter_curr - counter_prev) / elapsed_seconds`
- **Reset Protection**: Decreases in monotonic counters (from VM reboots or migrations) are detected and dropped rather than causing negative values or artificial spikes.
- **Fixed-Capacity Ring Buffers**: Stores 300 timestamped samples per VM per metric (`store.RingBuffer`) using a circular slice with zero allocations during steady-state `Push()`.
- **Node Migration Reset**: Informer migration events reset VM rate trackers to prevent inter-node metric contamination.

### 3. Unified Query Layer (`internal/query`)
- **Windowed Statistical Summaries**: Window queries calculate `min`, `avg`, `max`, `p95` (nearest-rank algorithm), and `last` across points within `[now - window, now]`.
- **Informer Context Enrichment**: Joins Kubernetes metadata directly onto metric payloads (vCPUs, CPU topology, memory limits, instance type, preference, eviction strategy, conditions, and volumes with PVC claims and capacity).
- **JSON Contract (`kvtop/v1`)**:
  - Explicit units in field names (`cpu_cores_used`, `mem_guest_used_bytes`, `net_rx_bytes_per_s`, `disk_latency_ms`).
  - Source attribution per metric (`virt-handler`, `kubernetes`, `virsh`).
  - Null representation for unavailable data with a sibling reason field (e.g. `"mem_guest_used_bytes": null`, `"mem_guest_used_bytes_reason": "no balloon stats"`).
  - Stale detection flagging age in seconds (`stale: true`, `stale_age_seconds: ...`).

### 4. Frontends
- **TUI (`internal/ui`)**: Consumes `query.SnapshotProvider` on each tick. Renders dynamic fluid columns that allocate remaining terminal width to VM names without truncation, Braille charts with threshold guidelines, and sparklines.
- **CLI (`internal/cli`)**: Non-interactive one-shot commands (`kvtop top`, `kvtop vm`, `kvtop nodes`). Implements window warmup (minimum 10s, default 15s) to ensure rates are calculated from distinct cache windows.
