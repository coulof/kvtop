<p align="center">
  <img src="docs/assets/kvtop-logo-wordmark.svg" alt="kvtop Logo" width="300">
</p>

<p align="center">
  <strong>btop-style terminal UI for live troubleshooting of Virtual Machines on KubeVirt clusters.</strong>
</p>

<p align="center">
  <a href="https://golang.org"><img src="https://img.shields.io/github/go-mod/go-version/coulof/kvtop" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>
</p>

---

`kvtop` is a terminal UI for live troubleshooting of Virtual Machines on KubeVirt clusters. Works with Harvester, SUSE Virtualization, OpenShift Virtualization, and upstream KubeVirt.

Built for live troubleshooting: see which VM is consuming resources right now, on which host, and in which namespace.

<p align="center">
  <img src="docs/assets/dashboard.png" alt="kvtop Dashboard Preview" width="800">
</p>

<p align="center">
  <img src="docs/assets/detail.png" alt="kvtop VM Drilldown Preview" width="800">
</p>

---

## Features

- **Terminal UI**: Live braille graphs and sparklines for VM CPU, memory overcommit, network throughput, and disk IOPS.
- **Client-side scraping**: Requires only standard `kubeconfig` credentials. Scrapes node `virt-handler` daemons in parallel through the Kubernetes API server pod proxy.
- **On-demand drilldown**: Streams live `virsh domstats` for the selected VM to inspect per-vCPU wait time and queue delay, per-NIC throughput, and per-disk IOPS.
- **Live migration tracking**: Watches KubeVirt informers to reflect VM phase transitions, host placements, and migrations immediately.

---

## Installation

### Pre-Built Binaries (GitHub Releases)

Download pre-compiled archives for Linux, macOS, or Windows from [GitHub Releases](https://github.com/coulof/kvtop/releases):

**macOS (Apple Silicon)**:

```bash
curl -LO https://github.com/coulof/kvtop/releases/latest/download/kvtop_v0.1.0_darwin_arm64.tar.gz
tar -xzf kvtop_v0.1.0_darwin_arm64.tar.gz
sudo mv kvtop /usr/local/bin/
```

**Linux (amd64)**:

```bash
curl -LO https://github.com/coulof/kvtop/releases/latest/download/kvtop_v0.1.0_linux_amd64.tar.gz
tar -xzf kvtop_v0.1.0_linux_amd64.tar.gz
sudo mv kvtop /usr/local/bin/
```

### Building From Source

Requires Go 1.26+:

```bash
git clone https://github.com/coulof/kvtop.git
cd kvtop
make build
./bin/kvtop
```

---

## Usage

```bash
# Launch interactive TUI using current kubeconfig
kvtop

# Specify kubeconfig and custom scrape interval
kvtop --kubeconfig ~/.kube/config --interval 3s

# Filter by namespace on startup
kvtop --namespace default

# Specify virt-handler namespace (defaults to harvester-system; use kubevirt or openshift-cnv)
kvtop --virt-handler-namespace kubevirt

# Sort by memory or network
kvtop --sort mem
kvtop --sort net

# Plain text mode for scripts or cron
kvtop --plain-text --count 2

# Offline replay mode with recorded fixtures
kvtop --replay testdata/
```

---

## CLI & Agent Subcommands

In addition to the interactive TUI, `kvtop` provides non-interactive subcommands outputting structured, versioned JSON (`"schema": "kvtop/v1"`) or human-readable tables (`-o table`):

```bash
# List top VMs sorted by CPU, memory, network, or disk
kvtop top --sort cpu -n 10 -o json
kvtop top --sort mem --ns default -o table

# Inspect a specific VM with CPU topology, memory specs, conditions, and volumes
kvtop vm default/coriolis-win-minion -o json
kvtop vm default/coriolis-win-minion -o table

# Inspect physical cluster nodes, VM density, and memory overcommit
kvtop nodes -o json
kvtop nodes -o table

# Run deterministic health diagnosis rules (CPU saturation, memory pressure, disk latency, imbalance)
kvtop diagnose -o json
kvtop diagnose --ns default -o table

# Record live cluster metrics for offline replay and anonymized testing
kvtop record --out ./fixtures --duration 10m --anonymize
```

---

## Model Context Protocol (MCP)

`kvtop` includes a built-in [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server (`kvtop mcp`) that communicates over stdio using JSON-RPC 2.0.

Because `kvtop mcp` runs as a long-lived process, it maintains warm in-memory ring buffers in the background. Tools answer AI agent queries immediately with zero latency and complete trend summaries (`min`, `avg`, `max`, `p95`, `last`).

### Available MCP Tools

| Tool | Description | Key Parameters |
|---|---|---|
| `top` | List top VMs by resource consumption | `sort` (cpu, mem, net, disk), `limit`, `namespaces`, `node`, `window` |
| `vm` | Deep inspection of a specific VM | `namespace`, `name`, `window`, `include_samples`, `allow_exec` |
| `nodes` | Physical host capacity and overcommit | `limit`, `window` |
| `diagnose` | Deterministic performance & health rule evaluation | `target_vm`, `namespaces`, `node`, `window`, `threshold_*` |

### Setting Up with AI Tools

#### 1. OpenCode

Add to your project's `opencode.json` (or `~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "kvtop": {
      "type": "local",
      "command": ["kvtop", "mcp"]
    }
  }
}
```

If your cluster requires a specific kubeconfig:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "kvtop": {
      "type": "local",
      "command": ["kvtop", "mcp", "--kubeconfig", "/path/to/kubeconfig"]
    }
  }
}
```

#### 2. Claude Code

Add `kvtop` using the Claude Code CLI:

```bash
# Add kvtop MCP server
claude mcp add kvtop -- kvtop mcp

# With custom kubeconfig or options
claude mcp add kvtop -- kvtop mcp --kubeconfig /path/to/kubeconfig
```

Or configure via `.mcp.json` in your repository root:

```json
{
  "mcpServers": {
    "kvtop": {
      "command": "kvtop",
      "args": ["mcp"]
    }
  }
}
```

#### 3. agy

Add `kvtop` using the `agy mcp` command:

```bash
# Add kvtop MCP server
agy mcp add kvtop kvtop mcp

# With custom kubeconfig or options
agy mcp add kvtop -- kvtop mcp --kubeconfig /path/to/kubeconfig
```

---

## Verification & Load Testing

The `scripts/load-generator.sh` script runs stress workloads on guest VMs through the QEMU guest agent without SSH keys:

```bash
# View VM connectivity
./scripts/load-generator.sh status --vm spike-it

# Generate CPU stress (watch sparklines turn red and CPU graph spike)
./scripts/load-generator.sh cpu --vm spike-it --duration 30

# Generate Memory stress (watch memory graph cross the 90% threshold line)
./scripts/load-generator.sh mem --vm spike-it --mb 3500 --duration 30

# Generate Storage IOPS stress
./scripts/load-generator.sh disk --vm spike-it --duration 30

# Trigger a Live Migration (observe real-time migration badges and host moves)
./scripts/load-generator.sh migrate --vm spike-it

# Clean up stress processes
./scripts/load-generator.sh clean --vm spike-it
```

---

## Documentation

- [User Manual & Metric Reference](docs/manual.md): Comprehensive guide to metric semantics, memory overcommit, proxy architecture, and full keybindings list.
- [Architecture & Internal Design](docs/architecture.md): Deep-dive into the two-frontend single-core architecture, query engine, ring buffer memory layout, and agent JSON contract.

---

## License

[MIT](LICENSE) © 2026 Florian Coulombel
