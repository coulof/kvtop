# kvtop Manual & Architecture Reference

`kvtop` (KubeVirt top) is a terminal UI for live troubleshooting of Virtual Machines on KubeVirt clusters (Harvester, SUSE Virtualization, OpenShift Virtualization, and upstream KubeVirt).

---

## 1. Architecture & Design Principles

- **Client-Side Only**: Runs standalone (`kvtop`). No agents, DaemonSets, CRDs, or container images are installed in the cluster.
- **Air-Gapped Ready**: Requires only an admin `kubeconfig`. Works completely without Prometheus or external monitoring stacks.
- **Zero Cluster Impact**:
  - VM metrics are scraped directly from node `virt-handler` pods via the Kubernetes API server pod proxy (`https:<pod>:8443/proxy/metrics`) in parallel (one request per physical node, never one per VM).
  - Detailed drilldown (`virsh domstats`) is streamed on-demand only for the selected VM while the detail view is open.
- **Real-Time Informers**: Watches `VirtualMachineInstance`, `VirtualMachineInstanceMigration`, `Node`, and `Namespace` resources to immediately reflect VM phase changes, node migrations, and capacity updates.

---

## 2. Dashboard Panels Explained

<p align="center">
  <img src="assets/dashboard.png" alt="kvtop Dashboard Overview" width="800">
</p>

### Top Bar
- **Cluster & Version**: Cluster context and Kubernetes API server version.
- **Nodes**: Number of ready physical hosts over total cluster hosts.
- **VMs**: Running VM count over total configured VMs.
- **Migration Alert (`⇶ N migrating`)**: Displays in real time whenever live migrations are in progress.
- **Namespace & Interval**: Active namespace scope and current polling frequency.

### Total VM CPU *(Top-Left Panel)*
- **Scope**: Cluster-wide sum of all VM workloads (or scoped to the selected namespace).
- **Metric**: Live cores used over total allotted vCPUs (`coresUsed / allottedVCPUs`), with percentage saturation.
- **Chart**: 2D Unicode braille time series history (300 data points).

### Nodes Panel *(Top-Right Panel)*
- **Scope**: Cluster-wide physical host overview (always remains cluster-wide even when filtering by namespace).
- **Metrics Per Host**:
  - **VM Count**: In-line horizontal bar and count (`■■··· 3VM` or `■■■·· 4⇶` during migration).
  - **CPU**: VM core utilization and visual progress bar.
  - **Memory Overcommit**: Allocated VM memory over node allocatable memory as a percentage and progress bar.
  - **Network**: Aggregate VM traffic passing through this host (`▼RX ▲TX`).
  - **Storage**: Aggregate VM block device IOPS on this host.

### Total VM Memory (Guest) *(Bottom-Left Top Panel)*
- **Scope**: Cluster-wide guest-used RAM derived from virtio balloon stats.
- **Threshold**: Dashed horizontal guideline at 90% capacity to highlight memory pressure.
- **Overcommit**: Overall cluster memory overcommit ratio.

### Total VM Network *(Bottom-Left Bottom Panel)*
- **Scope**: Aggregate VM virtual interface traffic (`▼RX` receive / `▲TX` transmit).
- **Semantics**: Excludes host, management, storage replication, and migration traffic.

### VMs Table *(Bottom-Right Panel)*
- **Fluid Layout**: Dynamically expands `NAME` and `NAMESPACE` columns to available terminal width to prevent name truncation.
- **NAMESPACE**: Kubernetes namespace.
- **NAME**: VM identifier (`⇶` prefix indicates active live migration).
- **NODE**: Host node placement (`⇶<node>` indicates migration target node).
- **CPU (HIST+USED)**: Braille sparkline plus cores used over allotted vCPUs (e.g. `⡠⠤⠒⠒ 0.58/4`).
- **MEM GUEST%**: Guest-used memory percentage and used bytes. Displays `–` if the guest does not run balloon drivers.
- **NET**: Live receive (`▼`) and transmit (`▲`) throughput.
- **IOPS**: Real-time read and write disk operations per second.

---

## 3. VM Detail View (Drilldown)

<p align="center">
  <img src="assets/detail.png" alt="kvtop VM Detail Drilldown" width="800">
</p>

Select a VM in the table and press `Enter` to open the detail view:

- **Host Footprint (Launcher RSS)**:
  - Separates launcher RSS (host hypervisor cost) from guest-allocated RAM.
  - Shows balloon current, usable, available, and unused bytes.
- **Per-vCPU Breakdown**:
  - Per-core utilization % with live braille sparkline.
  - Cumulative execution time, hypervisor wait time, and queue delay to spot hypervisor CPU contention.
- **Per-NIC Breakdown**:
  - Real-time throughput rates (`RX/s`, `TX/s`), cumulative traffic, and packet/drop/error counts.
- **Per-Disk Breakdown**:
  - Real-time read/write IOPS, read/write throughput, capacity, and flush counts.

---

## 4. Keybindings

| Key | Action |
|---|---|
| `Enter` | Open Detail View for selected VM (or scope node if Nodes panel is focused) |
| `Space` | Toggle host scope filter when Nodes panel is focused |
| `Esc` / `q` | Close Detail View / cancel search / clear filters |
| `Tab` | Toggle focus between VM Table and Nodes Panel |
| `o` | Sort by Namespace / VM Name (alphabetical default) |
| `c` | Sort by CPU utilization |
| `m` | Sort by Guest Memory usage |
| `n` | Sort by Network throughput |
| `d` | Sort by Disk IOPS |
| `s` | Toggle CPU sort: absolute cores (`1.80c`) vs saturation (`45%`) |
| `r` | Reverse sort order |
| `←` / `→` | Cycle sort column |
| `/` | Fuzzy search filter by VM name or namespace (`Enter` to apply, `Esc` to clear) |
| `+` / `-` | Increase / decrease refresh interval |
| `?` | Toggle in-app Documentation & Help overlay |
| `Ctrl+C` | Quit kvtop |

---

## 5. Offline Replay Mode

To develop, demo, or test without cluster connectivity, run with recorded fixtures:

```bash
./bin/kvtop --replay testdata/
```
