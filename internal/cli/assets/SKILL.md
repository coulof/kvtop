---
name: kvtop
description: Troubleshoot Virtual Machines on KubeVirt and Harvester clusters using the kvtop CLI. Trigger when diagnosing high VM CPU, memory pressure, storage IOPS latency, noisy neighbors, or live migration issues.
---

# kvtop Agent Skill

Use `kvtop` to inspect live resource usage and diagnose performance issues on KubeVirt and Harvester clusters.

## Command Execution Rules

1. **Always pass `-o json`** to receive structured output adhering to schema `kvtop/v1`.
2. **Always pass `-q` (`--quiet`)** to suppress stderr progress bars and keep command output clean.
3. **Respect sampling windows**: `virt-handler` caches libvirt domain statistics for ~5 seconds. One-shot commands block for `--window` (default 15s, minimum 10s) to calculate real counter deltas.
4. **Missing metrics**: Missing data is represented as `null` with a sibling `*_reason` field (for example, `"mem_guest_used_bytes": null` with `"mem_guest_used_bytes_reason": "no balloon stats"`). Never assume null means zero.
5. **Stale flags**: If a VM or node has not reported new scrapes recently, `stale: true` is set alongside `stale_age_seconds`.

## Quick Command Reference

```bash
# List top VMs by resource usage (cpu, mem, net, disk)
kvtop top --sort cpu -n 10 -o json -q
kvtop top --sort cpu --by-saturation -n 10 -o json -q
kvtop top --sort mem --ns default -n 5 -o json -q

# Inspect a single VM (CPU topology, memory specs, conditions, volumes)
kvtop vm <namespace>/<name> -o json -q

# Inspect physical nodes, VM density, and memory overcommit
kvtop nodes -o json -q

# Run deterministic diagnosis rules across cluster, namespace, node, or single VM
kvtop diagnose -o json -q
kvtop diagnose <namespace>/<name> -o json -q
kvtop diagnose --node <node-name> -o json -q
kvtop diagnose --ns <namespace> -o json -q

# Offline replay against recorded fixtures
kvtop <command> --replay <fixture-dir> -o json -q
```

## Troubleshooting Playbooks

### Playbook A: Find Noisy Neighbors
1. Run `kvtop top --sort cpu -n 5 -o json -q` to identify high CPU consumers.
2. Run `kvtop top --sort disk -n 5 -o json -q` to check for storage I/O thrashing.
3. Run `kvtop diagnose -o json -q` to look for `node_imbalance` findings. If present, the evidence lists the top contributing VMs causing the imbalance.

### Playbook B: Triage an Unhealthy VM
1. Run `kvtop vm <namespace>/<name> -o json -q`.
2. Check `cpu_saturation_percent.avg`: values above 90% indicate the guest needs more allotted vCPUs.
3. Check `mem_guest_used_bytes`:
   - If `null` with reason `"no balloon stats"`, the QEMU guest agent / balloon driver is missing or stopped.
   - If populated, check `mem_guest_used_percent.avg`: values above 90% indicate guest memory pressure.
4. Inspect `context.conditions` for `Ready: False`, `LiveMigratable: False`, or storage errors.
5. Inspect `context.volumes` to identify backing PVC claims and volume modes.
6. Run `kvtop diagnose <namespace>/<name> -o json -q` to retrieve deterministic findings and evidence.

### Playbook C: Check Host Sizing & Overcommit
1. Run `kvtop nodes -o json -q`.
2. Inspect `overcommit_ratio` (`mem_allocated_bytes / node_allocatable_bytes`):
   - Overcommit above 1.5 (150%) triggers a warning finding.
   - Overcommit above 2.0 (200%) triggers a critical finding.
3. Compare `allotted_vcpus` against physical `cpu_allocatable_cores` to verify CPU density.

### Playbook D: Diagnose Stalled Migrations
1. Run `kvtop diagnose -o json -q`.
2. Inspect any findings with ID `migration_not_converging`.
3. Check `vm.migration`: verify `source_node`, `target_node`, and `phase`. If migration dirty rate exceeds network transfer rate, the migration cannot converge while the guest workload remains active.
