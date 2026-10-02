#!/usr/bin/env bash
#
# load-generator.sh: Generate CPU, memory, disk, and live migration loads on a Harvester VM
# to verify kvtop live monitoring and metrics accuracy.
#
# Uses QEMU guest agent (virsh qemu-agent-command in virt-launcher) - no SSH keys required.
#

set -euo pipefail

KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
NAMESPACE="default"
VM_NAME="spike-it"
DURATION=30
MEM_MB=3500
CORES=""

usage() {
    cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  cpu       Generate 100% CPU saturation on all VM cores
  mem       Allocate and dirty memory (crosses the 90% threshold line)
  disk      Generate direct I/O read and write stress
  all       Generate concurrent CPU, memory, and disk stress
  migrate   Trigger a live migration to another cluster host
  clean     Kill all background stress processes on the target VM
  status    Show target VM status, IP, host node, and agent connectivity

Options:
  --vm <name>         Target VM name (default: spike-it)
  -n, --namespace <ns> Namespace (default: default)
  -d, --duration <sec> Stress duration in seconds (default: 30)
  -m, --mb <size>     Memory to dirty in MB for 'mem' load (default: 3500)
  -c, --cores <count> CPU worker count (default: all allotted cores)
  -k, --kubeconfig <f> Path to kubeconfig
  -h, --help          Show this help message

Examples:
  $(basename "$0") cpu --duration 45
  $(basename "$0") mem --mb 3800 --duration 60
  $(basename "$0") disk
  $(basename "$0") migrate
EOF
    exit 1
}

# Parse command
if [[ $# -eq 0 ]]; then
    usage
fi
COMMAND="$1"
shift

# Parse options
while [[ $# -gt 0 ]]; do
    case "$1" in
        --vm)
            VM_NAME="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -d|--duration)
            DURATION="$2"
            shift 2
            ;;
        -m|--mb)
            MEM_MB="$2"
            shift 2
            ;;
        -c|--cores)
            CORES="$2"
            shift 2
            ;;
        -k|--kubeconfig)
            KUBECONFIG="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

export KUBECONFIG

get_launcher_pod() {
    local pod
    pod=$(kubectl get pods -n "$NAMESPACE" -l "vm.kubevirt.io/name=$VM_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [[ -z "$pod" ]]; then
        pod=$(kubectl get pods -n "$NAMESPACE" -l "vmi.kubevirt.io/id=$VM_NAME" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    fi
    if [[ -z "$pod" ]]; then
        echo "Error: no running virt-launcher pod found for VM $NAMESPACE/$VM_NAME" >&2
        exit 1
    fi
    echo "$pod"
}

get_allotted_cores() {
    local c
    c=$(kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o jsonpath='{.spec.domain.cpu.cores}' 2>/dev/null || true)
    if [[ -z "$c" || "$c" -le 0 ]]; then
        c=2
    fi
    echo "$c"
}

guest_exec() {
    local cmd="$1"
    local pod
    pod=$(get_launcher_pod)
    local domain="default_${VM_NAME}"

    # Escape JSON
    local escaped_cmd
    escaped_cmd=$(python3 -c "import json, sys; print(json.dumps(sys.argv[1]))" "$cmd")

    kubectl exec -n "$NAMESPACE" "$pod" -c compute -- virsh qemu-agent-command "$domain" \
        "{\"execute\":\"guest-exec\",\"arguments\":{\"path\":\"/usr/bin/bash\",\"arg\":[\"-c\",$escaped_cmd]}}" >/dev/null
}

case "$COMMAND" in
    status)
        echo "=== VM Status: $NAMESPACE/$VM_NAME ==="
        kubectl get vmi -n "$NAMESPACE" "$VM_NAME" -o wide
        pod=$(get_launcher_pod)
        echo "Launcher Pod: $pod"
        echo -n "Guest Agent:  "
        kubectl exec -n "$NAMESPACE" "$pod" -c compute -- virsh qemu-agent-command "default_${VM_NAME}" '{"execute":"guest-ping"}' 2>/dev/null && echo "Connected" || echo "Not Connected"
        ;;

    cpu)
        if [[ -z "$CORES" ]]; then
            CORES=$(get_allotted_cores)
        fi
        echo ">>> Launching CPU stress on $VM_NAME ($CORES cores) for ${DURATION}s..."
        echo ">>> Observe in kvtop: CPU sparkline turning red (100% saturation) and Total VM CPU graph spiking."
        guest_exec "python3 -c \"import time, multiprocessing as mp; [mp.Process(target=lambda: [x*x for x in iter(int, 1)]).start() for _ in range($CORES)]; time.sleep($DURATION)\""
        echo ">>> Workload active in background. Run './scripts/load-generator.sh clean' to stop early."
        ;;

    mem)
        echo ">>> Launching Memory stress on $VM_NAME (${MEM_MB}MB) for ${DURATION}s..."
        echo ">>> Observe in kvtop: Balloon available memory dropping and Total VM Memory crossing 90% threshold line."
        guest_exec "python3 -c \"import time; a = bytearray($MEM_MB * 1024 * 1024); [a.__setitem__(i, 1) for i in range(0, len(a), 4096)]; time.sleep($DURATION)\""
        echo ">>> Workload active in background. Run './scripts/load-generator.sh clean' to stop early."
        ;;

    disk)
        echo ">>> Launching Disk Direct I/O stress on $VM_NAME for ${DURATION}s..."
        echo ">>> Observe in kvtop: IOPS column jumping and Host storage IOPS climbing."
        guest_exec "end=\$(( \$(date +%s) + $DURATION )); while [ \$(date +%s) -lt \$end ]; do dd if=/dev/zero of=/tmp/loadtest bs=1M count=100 oflag=direct conv=notrunc 2>/dev/null; dd if=/tmp/loadtest of=/dev/null bs=4k count=5000 iflag=direct 2>/dev/null; done; rm -f /tmp/loadtest"
        echo ">>> Workload active in background. Run './scripts/load-generator.sh clean' to stop early."
        ;;

    all)
        if [[ -z "$CORES" ]]; then
            CORES=$(get_allotted_cores)
        fi
        echo ">>> Launching concurrent CPU + Memory (${MEM_MB}MB) + Disk stress on $VM_NAME for ${DURATION}s..."
        guest_exec "python3 -c \"import time, multiprocessing as mp; a = bytearray($MEM_MB * 1024 * 1024); [a.__setitem__(i, 1) for i in range(0, len(a), 4096)]; [mp.Process(target=lambda: [x*x for x in iter(int, 1)]).start() for _ in range($CORES)]; time.sleep($DURATION)\" & end=\$(( \$(date +%s) + $DURATION )); while [ \$(date +%s) -lt \$end ]; do dd if=/dev/zero of=/tmp/loadtest bs=1M count=50 oflag=direct conv=notrunc 2>/dev/null; dd if=/tmp/loadtest of=/dev/null bs=4k count=2000 iflag=direct 2>/dev/null; done; rm -f /tmp/loadtest"
        echo ">>> Full stress active in background."
        ;;

    migrate)
        echo ">>> Triggering Live Migration for VM $NAMESPACE/$VM_NAME..."
        if command -v virtctl >/dev/null 2>&1; then
            virtctl migrate -n "$NAMESPACE" "$VM_NAME"
        else
            cat <<EOF | kubectl apply -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstanceMigration
metadata:
  generateName: mig-${VM_NAME}-
  namespace: ${NAMESPACE}
spec:
  vmiName: ${VM_NAME}
EOF
        fi
        echo ">>> Observe in kvtop: Purple '⇶ 1 migrating' alert in header, '⇶' badge in table, and node in/out counters."
        ;;

    clean)
        echo ">>> Cleaning up stress processes on $VM_NAME..."
        guest_exec "pkill -f python3 2>/dev/null || true; pkill -f dd 2>/dev/null || true; rm -f /tmp/loadtest"
        echo ">>> Cleanup complete."
        ;;

    *)
        echo "Unknown command: $COMMAND"
        usage
        ;;
esac
