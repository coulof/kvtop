package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// RunCompletion handles the `kvtop completion <shell>` subcommand.
func RunCompletion(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		PrintJSONError(stdout, fmt.Errorf("shell argument required: 'bash', 'zsh', or 'fish'"))
		return 1
	}

	shell := strings.ToLower(strings.TrimSpace(args[0]))
	switch shell {
	case "bash":
		fmt.Fprint(stdout, bashCompletionScript)
		return 0
	case "zsh":
		fmt.Fprint(stdout, zshCompletionScript)
		return 0
	case "fish":
		fmt.Fprint(stdout, fishCompletionScript)
		return 0
	default:
		PrintJSONError(stdout, fmt.Errorf("unsupported shell '%s'; supported shells are 'bash', 'zsh', 'fish'", shell))
		return 1
	}
}

const bashCompletionScript = `# bash completion for kvtop                                 -*- shell-script -*-

_kvtop() {
    local cur prev words cword
    _init_completion || return

    local commands="top vm nodes diagnose record mcp skill completion help"
    local common_flags="--window --interval -o --output --kubeconfig --replay --virt-handler-namespace -q --quiet"

    # Complete subcommands at first positional argument
    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
        return 0
    fi

    local subcmd="${words[1]}"

    case "${prev}" in
        --sort|-sort)
            COMPREPLY=( $(compgen -W "cpu mem net disk" -- "${cur}") )
            return 0
            ;;
        -o|--output|-output)
            COMPREPLY=( $(compgen -W "json table" -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            return 0
            ;;
        --ns|-ns|--namespace|-namespace)
            if command -v kubectl >/dev/null 2>&1; then
                local namespaces=$(kubectl get namespaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
                COMPREPLY=( $(compgen -W "${namespaces}" -- "${cur}") )
            fi
            return 0
            ;;
        --node|-node)
            if command -v kubectl >/dev/null 2>&1; then
                local nodes=$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
                COMPREPLY=( $(compgen -W "${nodes}" -- "${cur}") )
            fi
            return 0
            ;;
        --replay|-replay|--out|-out|--kubeconfig|-kubeconfig)
            _filedir
            return 0
            ;;
    esac

    # Complete flags for subcommands
    case "${subcmd}" in
        top)
            local top_flags="${common_flags} --sort --by-saturation --saturation --ns --namespace --node -n --limit --samples"
            COMPREPLY=( $(compgen -W "${top_flags}" -- "${cur}") )
            return 0
            ;;
        vm)
            if [[ ${cur} == -* ]]; then
                local vm_flags="${common_flags} --samples --allow-exec"
                COMPREPLY=( $(compgen -W "${vm_flags}" -- "${cur}") )
            else
                if command -v kubectl >/dev/null 2>&1; then
                    local vmis=$(kubectl get vmi -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{" "}{end}' 2>/dev/null)
                    COMPREPLY=( $(compgen -W "${vmis}" -- "${cur}") )
                fi
            fi
            return 0
            ;;
        nodes)
            local nodes_flags="${common_flags} -n --limit"
            COMPREPLY=( $(compgen -W "${nodes_flags}" -- "${cur}") )
            return 0
            ;;
        diagnose)
            if [[ ${cur} == -* ]]; then
                local diag_flags="${common_flags} --ns --namespace --node --threshold-cpu --threshold-mem --threshold-latency --threshold-overcommit --threshold-imbalance --threshold-stale"
                COMPREPLY=( $(compgen -W "${diag_flags}" -- "${cur}") )
            else
                if command -v kubectl >/dev/null 2>&1; then
                    local vmis=$(kubectl get vmi -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{" "}{end}' 2>/dev/null)
                    COMPREPLY=( $(compgen -W "${vmis}" -- "${cur}") )
                fi
            fi
            return 0
            ;;
        record)
            local rec_flags="--out --duration --anonymize --replay --kubeconfig --virt-handler-namespace --interval"
            COMPREPLY=( $(compgen -W "${rec_flags}" -- "${cur}") )
            return 0
            ;;
        mcp)
            local mcp_flags="--allow-exec --replay --kubeconfig --virt-handler-namespace --interval"
            COMPREPLY=( $(compgen -W "${mcp_flags}" -- "${cur}") )
            return 0
            ;;
        skill)
            COMPREPLY=( $(compgen -W "--out" -- "${cur}") )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            return 0
            ;;
    esac
}

complete -F _kvtop kvtop
`

const zshCompletionScript = `#compdef kvtop

_kvtop() {
    local -a commands
    commands=(
        'top:List top VMs sorted by metric'
        'vm:Inspect a specific VM'
        'nodes:List cluster hosts and capacity'
        'diagnose:Run deterministic health diagnosis'
        'record:Record cluster metrics to directory for replay'
        'mcp:Start Model Context Protocol (MCP) server'
        'skill:Generate AI agent skill runbook'
        'completion:Generate shell completion script'
        'help:Show help message'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    local subcmd="${words[2]}"
    case "$subcmd" in
        top)
            _arguments \
                '--sort[Metric to sort by]:metric:(cpu mem net disk)' \
                '--by-saturation[Sort CPU by saturation percent]' \
                '--ns[Filter by namespace]:namespace:__kvtop_namespaces' \
                '--namespace[Filter by namespace]:namespace:__kvtop_namespaces' \
                '--node[Filter by node]:node:__kvtop_nodes' \
                '(-n --limit)'{-n,--limit}'[Limit results]:count:' \
                '--window[Metrics sampling window]:window:(10s 15s 30s 1m 5m)' \
                '(-o --output)'{-o,--output}'[Output format]:format:(json table)' \
                '--samples[Include raw samples]' \
                '(-q --quiet)'{-q,--quiet}'[Suppress progress updates]' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files' \
                '--interval[Scrape interval]:interval:(1s 2s 5s)'
            ;;
        vm)
            _arguments \
                '--window[Metrics sampling window]:window:(10s 15s 30s 1m 5m)' \
                '(-o --output)'{-o,--output}'[Output format]:format:(json table)' \
                '--samples[Include raw samples]' \
                '--allow-exec[Enable virsh exec]' \
                '(-q --quiet)'{-q,--quiet}'[Suppress progress updates]' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files' \
                '*:virtual machine:__kvtop_vmis'
            ;;
        nodes)
            _arguments \
                '(-n --limit)'{-n,--limit}'[Limit results]:count:' \
                '--window[Metrics sampling window]:window:(10s 15s 30s 1m 5m)' \
                '(-o --output)'{-o,--output}'[Output format]:format:(json table)' \
                '(-q --quiet)'{-q,--quiet}'[Suppress progress updates]' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files'
            ;;
        diagnose)
            _arguments \
                '--ns[Filter by namespace]:namespace:__kvtop_namespaces' \
                '--namespace[Filter by namespace]:namespace:__kvtop_namespaces' \
                '--node[Filter by node]:node:__kvtop_nodes' \
                '--window[Metrics sampling window]:window:(10s 15s 30s 1m)' \
                '(-o --output)'{-o,--output}'[Output format]:format:(json table)' \
                '(-q --quiet)'{-q,--quiet}'[Suppress progress updates]' \
                '--threshold-cpu[Override CPU saturation threshold]:ratio:' \
                '--threshold-mem[Override memory pressure threshold]:ratio:' \
                '--threshold-latency[Override disk latency threshold]:ms:' \
                '--threshold-overcommit[Override overcommit threshold]:ratio:' \
                '--threshold-imbalance[Override imbalance threshold]:ratio:' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files' \
                '*:virtual machine:__kvtop_vmis'
            ;;
        record)
            _arguments \
                '--out[Output directory]:directory:_files -/' \
                '--duration[Record duration]:duration:(1m 5m 10m)' \
                '--anonymize[Sanitize identifiers]' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files'
            ;;
        mcp)
            _arguments \
                '--allow-exec[Enable virsh exec]' \
                '--replay[Replay directory]:directory:_files -/' \
                '--kubeconfig[Kubeconfig path]:file:_files' \
                '--interval[Scrape interval]:interval:(1s 2s 5s)'
            ;;
        skill)
            _arguments \
                '--out[Output file]:file:_files'
            ;;
        completion)
            _arguments '1:shell:(bash zsh fish)'
            ;;
    esac
}

__kvtop_namespaces() {
    if command -v kubectl >/dev/null 2>&1; then
        local -a ns
        ns=($(kubectl get namespaces -o jsonpath='{.items[*].metadata.name}' 2>/dev/null))
        _describe 'namespace' ns
    fi
}

__kvtop_nodes() {
    if command -v kubectl >/dev/null 2>&1; then
        local -a nodes
        nodes=($(kubectl get nodes -o jsonpath='{.items[*].metadata.name}' 2>/dev/null))
        _describe 'node' nodes
    fi
}

__kvtop_vmis() {
    if command -v kubectl >/dev/null 2>&1; then
        local -a vmis
        vmis=($(kubectl get vmi -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{" "}{end}' 2>/dev/null))
        _describe 'virtual machine' vmis
    fi
}

compdef _kvtop kvtop
`

const fishCompletionScript = `# fish completion for kvtop

function __fish_kvtop_needs_command
    set cmd (commandline -opc)
    if [ (count $cmd) -eq 1 ]
        return 0
    end
    return 1
end

function __fish_kvtop_using_command
    set cmd (commandline -opc)
    if [ (count $cmd) -gt 1 ]
        if [ $argv[1] = $cmd[2] ]
            return 0
        end
    end
    return 1
end

# Commands
complete -c kvtop -n "__fish_kvtop_needs_command" -a "top" -d "List top VMs by resource usage"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "vm" -d "Inspect a specific VM"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "nodes" -d "List cluster hosts and capacity"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "diagnose" -d "Run deterministic health diagnosis"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "record" -d "Record cluster metrics for replay"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "mcp" -d "Start Model Context Protocol (MCP) server"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "skill" -d "Generate AI agent skill runbook"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "completion" -d "Generate shell completion script"
complete -c kvtop -n "__fish_kvtop_needs_command" -a "help" -d "Show help message"

# Top command flags
complete -c kvtop -n "__fish_kvtop_using_command top" -l sort -a "cpu mem net disk" -d "Metric to sort by"
complete -c kvtop -n "__fish_kvtop_using_command top" -l by-saturation -d "Sort CPU by saturation percent"
complete -c kvtop -n "__fish_kvtop_using_command top" -l ns -d "Filter by namespace"
complete -c kvtop -n "__fish_kvtop_using_command top" -l node -d "Filter by node"
complete -c kvtop -n "__fish_kvtop_using_command top" -s n -l limit -d "Limit results"
complete -c kvtop -n "__fish_kvtop_using_command top" -l window -d "Metrics sampling window"
complete -c kvtop -n "__fish_kvtop_using_command top" -s o -l output -a "json table" -d "Output format"
complete -c kvtop -n "__fish_kvtop_using_command top" -s q -l quiet -d "Suppress progress updates"

# Completion command
complete -c kvtop -n "__fish_kvtop_using_command completion" -a "bash zsh fish" -d "Target shell"
`
