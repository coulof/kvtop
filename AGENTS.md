# AGENTS.md

## Project

`kvtop` (KubeVirt top): a btop-style terminal UI showing live CPU, memory, network and disk usage of VMs on a Harvester / SUSE Virtualization cluster.

It has two frontends on one core: the TUI for humans (shipped in v0.1.0), and a JSON CLI / MCP server so an AI agent can query and diagnose VM performance (in progress).

Primary job: live troubleshooting. "Which VM is the noisy neighbour right now, on which node, in which namespace."

Target: Harvester v1.6+ (developed against v1.9), often air-gapped. The only thing the user is guaranteed to have is an admin kubeconfig.

## Hard constraints

- Must work with the `rancher-monitoring` addon **disabled** (it is off by default). Prometheus is never a required dependency.
- Nothing installed in the cluster for v1: no DaemonSet, no CRD, no image to mirror. Client-side only.
- Single static binary, no CGO. Usable standalone and as a kubectl plugin (`kubectl-kvtop`).
- Read-only. No VM actions (start, stop, migrate). Do not add them.
- The virsh exec path is the only intrusive operation. It is disabled in the JSON CLI and MCP modes unless `--allow-exec` is passed.
- No network access other than the Kubernetes API server from the kubeconfig.

## Stack

- Go (1.26+)
- `bubbletea` + `lipgloss` for the TUI
- Custom high-density 2D braille time-series and sparkline engine (`internal/ui/chart/`) with Lipgloss background blending
- `client-go` (dynamic client and informers; avoids heavy kubevirt client dependencies)
- `prometheus/common/expfmt` to parse the text exposition format

## Architecture

```
cmd/kvtop/            main, flags, kubeconfig loading
internal/collect/     Collector interface + implementations (virt-handler, virsh, replay)
internal/store/       ring buffers, rate computation, aggregation, overcommit
internal/kube/        informers (VMI, VMIM, Node, Namespace), pod proxy, exec streaming
internal/query/       shared read API over the store: top, vm, nodes, summaries (planned)
internal/diagnose/    deterministic findings with evidence (planned)
internal/cli/         non-interactive subcommands, JSON output (planned)
internal/mcp/         MCP server over stdio, thin wrapper over query and diagnose (planned)
internal/ui/          bubbletea models: header, cpu, nodes, mem, net, table, detail, help
internal/ui/chart/    custom 2D braille graph and sparkline rendering
```

Data flow: collectors push raw counter samples into the store on a tick; the store computes rates from deltas and keeps history; the UI reads snapshots from the store. The UI never talks to the cluster directly.

Target state: every frontend (TUI, CLI, MCP) reads through `internal/query`. No frontend talks to the cluster or the store directly, and no logic lives in only one frontend. Moving the existing TUI onto `internal/query` is part of milestone 7 and must not change its behaviour.

### Collectors

All implement one interface, roughly:

```go
type Collector interface {
    Name() string
    Collect(ctx context.Context) ([]Sample, error)
}
```

1. **virt-handler** (primary, VM metrics). For each `virt-handler` pod in `harvester-system`, GET `/metrics` through the API server pod proxy, in parallel, every 2s by default. Parse `kubevirt_vmi_*` series. One request per node, never one per VM.
2. **metrics-server** (node bars). `metrics.k8s.io` NodeMetrics, polled every 15s. It is stale by design; do not poll faster.
3. **virsh stream** (detail view only). One long-lived exec into the selected VM's `virt-launcher` pod, `compute` container, running a 1s loop of `virsh domstats`, parsed from the stream. Opened when the detail view opens, closed when it closes. Never one exec per poll, never for more than one VM at a time.
4. **prometheus** (not built yet; optional for the TUI, important for agents). Used when the addon is enabled, to backfill history and answer questions about time ranges older than the in-memory buffer.

### Informers

Watch VMI, VirtualMachineInstanceMigration, Node and Namespace. State changes (phase, node move, migration start and end) must appear in the UI immediately, independent of the metrics tick.

## Metrics

Expected series from virt-handler. **Dump a real scrape first and confirm the names and labels before coding against them**; they have changed across KubeVirt releases.

| Use | Series (expected) |
| --- | --- |
| CPU | `kubevirt_vmi_cpu_usage_seconds_total`, `kubevirt_vmi_vcpu_seconds_total` |
| Memory | `kubevirt_vmi_memory_available_bytes`, `kubevirt_vmi_memory_usable_bytes`, `kubevirt_vmi_memory_unused_bytes`, `kubevirt_vmi_memory_resident_bytes` |
| Network | `kubevirt_vmi_network_receive_bytes_total`, `kubevirt_vmi_network_transmit_bytes_total` |
| Disk | `kubevirt_vmi_storage_iops_read_total`, `kubevirt_vmi_storage_iops_write_total`, `kubevirt_vmi_storage_read_traffic_bytes_total`, `kubevirt_vmi_storage_write_traffic_bytes_total` |
| Migration | `kubevirt_vmi_migration_data_remaining_bytes`, `kubevirt_vmi_migration_data_processed_bytes`, transfer rate and dirty rate series |

Key each series by `namespace` + `name`; take `node` from the label or from the VMI informer.

### Semantics (do not change without asking)

- **CPU** is shown as cores used over vCPUs allotted, e.g. `3.4/4`. Not a bare percentage. Default sort is absolute cores used; a toggle sorts by saturation (used / allotted).
- **Memory** on the main page is guest-used, derived from balloon stats. If a VM reports no balloon stats, show `–`, never a guessed value. Launcher RSS (host cost) belongs in the detail view.
- **Rates** are computed client-side from counter deltas over the actual elapsed time between samples. Handle counter resets (VM restart, migration to another node) by dropping the sample, not by showing a negative or a spike.
- **Network panel** shows aggregate VM traffic only and must be labelled as such. Host, storage and migration traffic are not visible.
- **Overcommit** is the sum of VM memory over node allocatable memory.

## Main page layout

```
┌ <cluster>  <version> ─ nodes ready ─ VMs running/total ─ alerts ─ ns filter ─ interval ┐
┌ cpu ──────────────────────────────┬ nodes ────────────────────────────────┐
│ braille history, total VM CPU     │ HOST  VMS[bar+count]  CPU[bar]  MEM[bar]  NET IO│
├ mem ──────────────┬ vms ──────────┴───────────────────────────────────────┤
│ guest-used history│ NS  NAME(auto-adjusting)  IP  NODE  CPU(sparkline+used)│
│ overcommit ratio  │ MEM%  RX  TX  IOPS                                    │
├ net ──────────────┤                                                       │
│ tx up / rx down   │                                                       │
└───────────────────┴───────────────────────────────────────────────────────┘
 keybinding footer
```

- Nodes are the equivalent of btop's cores. Per-node VM density includes an in-line horizontal bar (`■■··· 3VM`).
- VMs Table features an auto-adjusting column layout that dynamically allocates remaining terminal width to the `NAME` (and `NAMESPACE`) column, avoiding name truncation.
- Only the CPU column gets a per-row sparkline. Other columns are plain numbers.
- Colour gradient green to yellow to red on bars and graphs.
- Below 100 columns, drop the left-hand mem and net panels and keep header, nodes and table.
- History: ring buffer of 300 samples per VM per metric.

### Keys

| Key | Action |
| --- | --- |
| `o` | sort by Namespace / VM Name (alphabetical default) |
| `c` `m` `n` `d` | sort by CPU, memory, network, disk |
| `←` `→` | cycle sort column |
| `r` | reverse sort |
| `s` | toggle CPU sort: absolute / saturation |
| `/` | fuzzy filter on VM name or namespace |
| `tab` | toggle focus between VM Table and Nodes Panel |
| `space` | toggle scoping table to focused node (when Nodes panel focused) |
| `enter` | open VM detail view (or scope table to node when Nodes panel focused) |
| `+` `-` | change refresh interval |
| `?` | toggle in-app documentation & help overlay |
| `q` / `Ctrl+C` | quit |

A namespace filter also rescopes the cpu, mem and net graphs. Node bars always stay cluster-wide.

## Agent interface

The same data, for an AI agent instead of a human. Built on `internal/query` and `internal/diagnose`; the CLI comes first, MCP wraps it.

### Commands

```
kvtop top      --sort cpu|mem|net|disk [--ns ...] [--node ...] [-n 10] [--window 15s] -o json
kvtop vm       <namespace>/<name> [--window 15s] -o json
kvtop nodes    [--window 15s] -o json
kvtop diagnose [<namespace>/<name> | --ns ... | --node ...] [--window 30s] -o json
kvtop record   --out <dir> [--duration 10m] [--anonymize]
kvtop mcp      (stdio; exposes top, vm, nodes, diagnose as tools)
```

- Rates need at least two distinct samples, and virt-handler caches domain stats for about 5s (see spike results). One-shot commands therefore block for `--window` (default 15s, minimum 10s) and say so in the output. A rate computed from a zero delta inside one cache window is not a sample.
- `kvtop mcp` is long-lived and keeps the ring buffers warm, so its tools answer immediately and can report trends over the buffer length.
- All commands also accept `--replay <dir>` to run against a recording.
- `-o json` is the contract. `-o table` is a convenience for humans.

### Output rules

- Stable, versioned schema (`"schema": "kvtop/v1"`). Changing a field name or unit is a breaking change.
- Units in the field name: `cpu_cores_used`, `mem_guest_used_bytes`, `net_rx_bytes_per_s`, `disk_latency_ms`.
- Every response carries `collected_at`, `window_seconds`, and the data source per metric (`virt-handler`, `metrics-server`, `prometheus`, `virsh`).
- Missing data is `null` with a sibling `*_reason` field (for example `"no balloon stats"`). Never emit zero for unknown.
- Stale data is flagged with its age, not dropped.
- Default to summaries over the window: `min`, `avg`, `max`, `p95`, `last`. Raw samples only with `--samples`.
- Every list is capped (`-n`, default 10) and reports `total` and `truncated`.
- Errors are JSON on stdout with a non-zero exit code, not prose on stderr only.

### Context joined to each VM

From the informers, so the agent can explain a number without a second tool call: vCPUs, memory, instance type and preference, dedicated CPU placement, node, volumes with storage class, run strategy, phase and conditions, active or recent migrations, recent warning events.

### Findings (`kvtop diagnose`)

Computed deterministically in Go. The agent interprets findings; it does not do the arithmetic. Each finding has: `id`, `severity` (info, warning, critical), `subject` (VM or node), `summary` (one sentence), `evidence` (the values and thresholds used), `window_seconds`.

Initial rule set:

| id | Condition |
| --- | --- |
| `cpu_saturated` | cores used / vCPUs allotted above 0.9 for most of the window |
| `mem_guest_pressure` | guest-used above 0.9 of guest memory, from balloon stats |
| `disk_latency_high` | I/O time delta / ops delta above threshold, per disk |
| `migration_not_converging` | dirty rate above transfer rate over the window |
| `node_imbalance` | one node's VM CPU or memory far above the cluster median; lists top contributors |
| `node_overcommit` | sum of VM memory over node allocatable above threshold |
| `metrics_missing` | VM running but no balloon stats, or virt-handler scrape failing for its node |

Thresholds are constants in one file, overridable by flags. Each rule has table-driven tests against fixtures. If a rule cannot be evaluated because data is missing, emit nothing for it rather than a guess; `metrics_missing` covers the gap.

Do not add rules that need host-side data (CPU steal, PSI, host NIC saturation). They are out of reach without a node agent.

### Recording

`kvtop record` extends the existing fixture capture (`make record`) into a user-facing subcommand: it writes raw scrapes plus informer snapshots to a directory that `--replay` can read. `--anonymize` replaces namespace, VM, node and volume names with stable pseudonyms (same input maps to the same output within one recording) and drops labels and annotations.

## Milestones

1. **Spike** [Completed]: fetch and parse virt-handler `/metrics` through the API proxy, print a sorted plain-text table to stdout. Validated on Harvester v1.36.3+rke2r1.
2. **Store** [Completed]: ring buffers, rates, reset handling, aggregation by node and namespace. Unit-tested with recorded scrapes.
3. **Main page** [Completed]: layout, table, fluid column auto-adjustment, sort, filters, node focus.
4. **Charts** [Completed]: braille history and sparklines.
5. **Informers** [Completed]: instant state changes, migrations indicator.
6. **Detail view** [Completed]: virsh stream, per-vCPU, per-disk, per-NIC, RSS.
7. **Agent CLI** [Completed]: `internal/query`, then `top`, `vm`, `nodes` with JSON output and the output rules above. Refactor the TUI to read through `internal/query` with no behaviour change.
8. **Diagnose** [Completed]: `internal/diagnose`, the initial rule set, `kvtop diagnose`.
9. **MCP and record** [Completed]: `kvtop mcp`, `kvtop record`, `--anonymize`.
10. **Prometheus backend**: history backfill and time-range queries.
11. **Backlog / Future**: Longhorn panel, alerts panel, namespace multi-select picker (`N`), tree mode (`t`).

Milestones 1–6 shipped in **Release `v0.1.0`**. Stop after each remaining milestone and report before starting the next.

## Validated spike results

Empirical results from testing on a live Harvester cluster:

- **virt-handler proxy auth**: Validated. Standard kubeconfig admin credentials authenticate via `/api/v1/namespaces/harvester-system/pods/https:<pod>:8443/proxy/metrics`.
- **Scrape duration**: A 2s polling interval is sustainable; concurrent scraping bounds libvirt pressure.
- **virt-handler domain stats caching**: Confirmed ~5s cache window. Client-side rates handle zero deltas and counter resets gracefully.
- **Metric names and labels**: Documented and verified against `registry.suse.com/suse/sles/16.0/virt-handler:1.8.4-9.1`.
- **metrics-server**: Present and operational by default on Harvester clusters.
- **virsh domstats in virt-launcher**: Validated working inside the `compute` container.

## Conventions

- `gofmt`, `go vet`, `golangci-lint` clean.
- Table-driven tests. Collectors are tested against recorded fixtures in `testdata/`, never a live cluster.
- A `--replay <dir>` flag feeds recorded scrapes into the UI so it can be developed and demoed without a cluster. Build this in milestone 2.
- Errors from one node or one VM never blank the whole UI; show the row or node as stale with its last-seen age.
- Every collector call has a context timeout shorter than the tick interval.
- No telemetry, no update checks.
- Commands: `make build`, `make test`, `make lint`, `make record` (capture fixtures from the current kubeconfig).

## Out of scope

VM actions, multi-cluster, host agent, guest-internal process lists, alert management, any web UI, any LLM call from inside kvtop (it serves agents, it does not embed one).
