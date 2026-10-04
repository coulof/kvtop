# MCP Prompt Guide

This guide lists prompts for AI coding agents (OpenCode, Claude Code, agy) connected to the `kvtop mcp` server.

Each section shows practical troubleshooting questions, which MCP tool the agent invokes, and what data it inspects.

---

## 1. Live Triage & Resource Contention

Find which VMs are driving cluster load or competing for CPU, memory, network, and storage IOPS.

### Prompts

- **"Which VM is using the most CPU right now?"**
  - *Tool*: `top` (`sort="cpu"`, `limit=5`)
  - *Data*: Cores used, allotted vCPUs, saturation percentage.

- **"Which VMs are hitting high CPU saturation relative to their allotted vCPUs?"**
  - *Tool*: `top` (`sort="cpu"`, `by_saturation=true`, `limit=10`)
  - *Data*: Saturation percent (`coresUsed / allottedVCPUs * 100`). Useful for spotting smaller 1-vCPU or 2-vCPU VMs pegged at 100%.

- **"Show the top 5 VMs by network traffic in the `production` namespace."**
  - *Tool*: `top` (`sort="net"`, `namespaces=["production"]`, `limit=5`)
  - *Data*: Aggregate virtual interface receive (`net_rx_bytes_per_s`) and transmit (`net_tx_bytes_per_s`) rates.

- **"Are any VMs on node `hv-04` generating high disk IOPS?"**
  - *Tool*: `top` (`sort="disk"`, `node="hv-04"`, `limit=10`)
  - *Data*: Combined storage read and write IOPS per VM.

---

## 2. Health & Performance Diagnosis

Run deterministic rules against cluster metrics without manual arithmetic.

### Prompts

- **"Run a health check on the cluster and summarize any performance issues."**
  - *Tool*: `diagnose` (`window="30s"`)
  - *Data*: Evaluates CPU saturation (>90%), guest memory pressure (>90%), disk latency (>50ms), node imbalance, memory overcommit (>150%), and missing metrics.

- **"Is node `hv-02` experiencing resource imbalance compared to the rest of the cluster?"**
  - *Tool*: `diagnose` (`node="hv-02"`)
  - *Data*: Compares node VM CPU and memory load against the cluster median. If imbalanced, lists top contributing VMs.

- **"Diagnose VM `default/coriolis-win-minion`. Is it healthy?"**
  - *Tool*: `diagnose` (`target_vm="default/coriolis-win-minion"`)
  - *Data*: Evaluates VM-scoped rules and surfaces warnings or missing balloon stats.

- **"Are there any live migrations failing to converge?"**
  - *Tool*: `diagnose`
  - *Data*: Evaluates `migration_not_converging` findings where active live migrations are stalled or failing.

---

## 3. VM Inspection & Context

Retrieve hardware topology, memory allocation, PVC volumes, and conditions for a single VM in a single call.

### Prompts

- **"Show the configuration and resource usage of VM `default/test2`."**
  - *Tool*: `vm` (`namespace="default"`, `name="test2"`)
  - *Data*: Sockets, cores, threads, dedicated CPU placement, guest memory limits, PVC claim names, volume modes, storage capacity, and status conditions (`Ready`, `LiveMigratable`).

- **"Is VM `default/tumbleweed-flint-florian` running out of memory inside the guest?"**
  - *Tool*: `vm` (`namespace="default"`, `name="tumbleweed-flint-florian"`)
  - *Data*: Balloon driver statistics (`mem_guest_used_bytes`, `mem_guest_total_bytes`, `mem_guest_used_percent`).

- **"Show the historical metric samples for `default/spike-it` over the last minute."**
  - *Tool*: `vm` (`namespace="default"`, `name="spike-it"`, `window="1m"`, `include_samples=true`)
  - *Data*: Full array of timestamped points for CPU, memory, network, and disk IOPS across the ring buffer.

---

## 4. Host Capacity & Memory Overcommit

Evaluate physical node density and cluster memory ratios.

### Prompts

- **"What is the memory overcommit ratio across our physical nodes?"**
  - *Tool*: `nodes`
  - *Data*: `overcommit_ratio` (`mem_allocated_bytes / node_allocatable_bytes`), physical allocatable memory, and VM density per host.

- **"Which node has the highest VM density right now?"**
  - *Tool*: `nodes`
  - *Data*: `running_vm_count` and `vm_count` per host.

- **"Can node `hv-01` fit another 16GB VM without exceeding 150% overcommit?"**
  - *Tool*: `nodes`
  - *Data*: Current allocated VM memory, node allocatable RAM, and headroom calculation.

---

## 5. Observability & Metric Hygiene

Identify monitoring gaps or stale metrics.

### Prompts

- **"Which running VMs are missing QEMU guest balloon drivers?"**
  - *Tool*: `diagnose`
  - *Data*: Findings with ID `metrics_missing` and severity `info` indicating balloon drivers are not reporting guest memory.

- **"Are any nodes or VMs reporting stale metrics?"**
  - *Tool*: `diagnose` or `top`
  - *Data*: Flags where `stale` is true and `stale_age_seconds` exceeds 10 seconds.
