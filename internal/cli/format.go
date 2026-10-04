package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/ui"
)

// ErrorResponse is printed to stdout on failures when running in JSON mode.
type ErrorResponse struct {
	Schema string `json:"schema"`
	Error  string `json:"error"`
}

// PrintJSONError outputs a schema-compliant error to w.
func PrintJSONError(w io.Writer, err error) {
	resp := ErrorResponse{
		Schema: query.SchemaVersion,
		Error:  err.Error(),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// PrintJSON marshals data as indented JSON to w.
func PrintJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// PrintTopTable outputs a human-readable table for query.TopResult.
func PrintTopTable(w io.Writer, res *query.TopResult) {
	truncStr := ""
	if res.Truncated {
		truncStr = fmt.Sprintf(" (Showing %d)", len(res.Items))
	}
	fmt.Fprintf(w, "COLLECTED: %s | WINDOW: %.0fs | TOTAL: %d VMs%s\n\n",
		res.CollectedAt, res.WindowSeconds, res.Total, truncStr)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tNAME\tIP\tNODE\tCPU (AVG/LAST/ALLOT)\tCPU SAT%\tMEM GUEST%\tRX\tTX\tIOPS")
	fmt.Fprintln(tw, "---------\t----\t--\t----\t--------------------\t--------\t----------\t--\t--\t----")

	for _, vm := range res.Items {
		ip := "–"
		if vm.IP != nil && *vm.IP != "" {
			ip = *vm.IP
		}

		cpuStr := "–"
		if vm.CPUCoresUsed != nil {
			cpuStr = fmt.Sprintf("%.2f/%.2f/%d", vm.CPUCoresUsed.Avg, vm.CPUCoresUsed.Last, vm.AllottedVCPUs)
		}

		cpuSatStr := "–"
		if vm.CPUSaturationPercent != nil {
			cpuSatStr = fmt.Sprintf("%.1f%%", vm.CPUSaturationPercent.Last)
		}

		memStr := "–"
		if vm.MemGuestUsedPercent != nil && vm.MemGuestUsedBytes != nil && vm.MemGuestTotalBytes != nil {
			memStr = fmt.Sprintf("%.1f%% (%s/%s)",
				vm.MemGuestUsedPercent.Last,
				ui.FormatBytes(vm.MemGuestUsedBytes.Last),
				ui.FormatBytes(float64(*vm.MemGuestTotalBytes)),
			)
		}

		rxStr := "–"
		if vm.NetRxBytesPerSec != nil {
			rxStr = ui.FormatRate(vm.NetRxBytesPerSec.Last)
		}

		txStr := "–"
		if vm.NetTxBytesPerSec != nil {
			txStr = ui.FormatRate(vm.NetTxBytesPerSec.Last)
		}

		iopsStr := "–"
		if vm.DiskIOPSTotal != nil {
			iopsStr = fmt.Sprintf("%.1f", vm.DiskIOPSTotal.Last)
		}

		staleStr := ""
		if vm.Stale {
			staleStr = fmt.Sprintf(" (stale %.0fs)", vm.StaleAgeSeconds)
		}

		migStr := ""
		if vm.IsMigrating && vm.Migration != nil {
			migStr = fmt.Sprintf(" [⇶ %s->%s]", vm.Migration.SourceNode, vm.Migration.TargetNode)
		}

		fmt.Fprintf(tw, "%s\t%s%s%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			vm.Namespace,
			vm.Name,
			staleStr,
			migStr,
			ip,
			vm.Node,
			cpuStr,
			cpuSatStr,
			memStr,
			rxStr,
			txStr,
			iopsStr,
		)
	}
	tw.Flush()
}

// PrintVMTable outputs detailed human-readable information for query.VMResult.
func PrintVMTable(w io.Writer, res *query.VMResult) {
	vm := res.VM
	fmt.Fprintf(w, "VIRTUAL MACHINE: %s/%s\n", vm.Namespace, vm.Name)
	fmt.Fprintf(w, "COLLECTED: %s | WINDOW: %.0fs\n\n", res.CollectedAt, res.WindowSeconds)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Node:\t%s\n", vm.Node)
	fmt.Fprintf(tw, "Phase:\t%s\n", vm.Phase)
	ipStr := "–"
	if vm.IP != nil {
		ipStr = *vm.IP
	}
	fmt.Fprintf(tw, "IP:\t%s\n", ipStr)
	fmt.Fprintf(tw, "Allotted vCPUs:\t%d\n", vm.AllottedVCPUs)

	if vm.Context != nil {
		fmt.Fprintf(tw, "CPU Topology:\t%d sockets, %d cores, %d threads\n",
			vm.Context.CPUTopology.Sockets, vm.Context.CPUTopology.Cores, vm.Context.CPUTopology.Threads)
		fmt.Fprintf(tw, "Dedicated CPU:\t%v\n", vm.Context.DedicatedCPUPlacement)
		if vm.Context.MemoryGuest != "" {
			fmt.Fprintf(tw, "Memory Guest:\t%s\n", vm.Context.MemoryGuest)
		}
		if vm.Context.InstanceType != "" {
			fmt.Fprintf(tw, "Instance Type:\t%s\n", vm.Context.InstanceType)
		}
		if vm.Context.Preference != "" {
			fmt.Fprintf(tw, "Preference:\t%s\n", vm.Context.Preference)
		}
		if vm.Context.EvictionStrategy != "" {
			fmt.Fprintf(tw, "Eviction Strategy:\t%s\n", vm.Context.EvictionStrategy)
		}
	}
	tw.Flush()

	fmt.Fprintln(w, "\nMETRICS OVER WINDOW:")
	twMetrics := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(twMetrics, "METRIC\tMIN\tAVG\tMAX\tP95\tLAST\tSOURCE")
	fmt.Fprintln(twMetrics, "------\t---\t---\t---\t---\t----\t------")

	printMetricRow(twMetrics, "CPU Cores Used", vm.CPUCoresUsed, "%.2f", vm.CPUCoresUsedSource, "")
	printMetricRow(twMetrics, "CPU Saturation %", vm.CPUSaturationPercent, "%.1f%%", vm.CPUCoresUsedSource, "")
	printMetricRow(twMetrics, "Guest Memory", vm.MemGuestUsedBytes, "%s", vm.MemGuestUsedBytesSource, vm.MemGuestUsedBytesReason)
	printMetricRow(twMetrics, "Net RX", vm.NetRxBytesPerSec, "%s/s", vm.NetRxBytesPerSecSource, "")
	printMetricRow(twMetrics, "Net TX", vm.NetTxBytesPerSec, "%s/s", vm.NetTxBytesPerSecSource, "")
	printMetricRow(twMetrics, "Disk IOPS", vm.DiskIOPSTotal, "%.1f", vm.DiskIOPSTotalSource, "")
	printMetricRow(twMetrics, "Disk Latency", vm.DiskLatencyMs, "%.2f ms", vm.DiskLatencyMsSource, vm.DiskLatencyMsReason)
	twMetrics.Flush()

	if vm.Context != nil && len(vm.Context.Volumes) > 0 {
		fmt.Fprintln(w, "\nVOLUMES:")
		twVol := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(twVol, "NAME\tCLAIM\tSTORAGE CLASS\tMODE\tCAPACITY")
		fmt.Fprintln(twVol, "----\t-----\t-------------\t----\t--------")
		for _, v := range vm.Context.Volumes {
			fmt.Fprintf(twVol, "%s\t%s\t%s\t%s\t%s\n",
				v.Name, v.ClaimName, v.StorageClass, v.VolumeMode, v.Capacity)
		}
		twVol.Flush()
	}

	if vm.Context != nil && len(vm.Context.Conditions) > 0 {
		fmt.Fprintln(w, "\nCONDITIONS:")
		twCond := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
		fmt.Fprintln(twCond, "TYPE\tSTATUS\tREASON\tMESSAGE")
		fmt.Fprintln(twCond, "----\t------\t------\t-------")
		for _, c := range vm.Context.Conditions {
			fmt.Fprintf(twCond, "%s\t%s\t%s\t%s\n", c.Type, c.Status, c.Reason, c.Message)
		}
		twCond.Flush()
	}
}

func printMetricRow(tw *tabwriter.Writer, name string, summary *query.MetricSummary, format, source, reason string) {
	if summary == nil {
		rStr := "null"
		if reason != "" {
			rStr = fmt.Sprintf("null (%s)", reason)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, rStr, rStr, rStr, rStr, rStr, source)
		return
	}

	formatVal := func(val float64) string {
		if strings.Contains(format, "%s") {
			return ui.FormatBytes(val)
		}
		return fmt.Sprintf(format, val)
	}

	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		name,
		formatVal(summary.Min),
		formatVal(summary.Avg),
		formatVal(summary.Max),
		formatVal(summary.P95),
		formatVal(summary.Last),
		source,
	)
}

// PrintNodesTable outputs a human-readable table for query.NodesResult.
func PrintNodesTable(w io.Writer, res *query.NodesResult) {
	fmt.Fprintf(w, "COLLECTED: %s | WINDOW: %.0fs | TOTAL NODES: %d\n\n",
		res.CollectedAt, res.WindowSeconds, res.Total)

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NODE\tREADY\tVMS (RUN/TOT)\tCPU USED\tALLOT/ALLOC CPU\tMEM GUEST\tMEM ALLOC\tOVERCOMMIT\tRX\tTX\tIOPS")
	fmt.Fprintln(tw, "----\t-----\t-------------\t--------\t---------------\t---------\t---------\t----------\t--\t--\t----")

	for _, n := range res.Items {
		readyStr := "Ready"
		if !n.Ready {
			readyStr = "NotReady"
		}

		vmStr := fmt.Sprintf("%d/%d", n.RunningVMCount, n.VMCount)
		cpuStr := fmt.Sprintf("%.2fc", n.CPUCoresUsed)
		cpuAllocStr := fmt.Sprintf("%d/%d", n.AllottedCPUs, n.CPUAllocatableCores)
		memGuestStr := ui.FormatBytes(float64(n.MemGuestUsedBytes))
		memAllocStr := ui.FormatBytes(float64(n.MemAllocatedBytes))
		ocStr := fmt.Sprintf("%.1f%%", n.OvercommitRatio*100)
		rxStr := ui.FormatRate(n.NetRxBytesPerSec)
		txStr := ui.FormatRate(n.NetTxBytesPerSec)
		iopsStr := fmt.Sprintf("%.1f", n.DiskIOPSTotal)

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			n.NodeName,
			readyStr,
			vmStr,
			cpuStr,
			cpuAllocStr,
			memGuestStr,
			memAllocStr,
			ocStr,
			rxStr,
			txStr,
			iopsStr,
		)
	}
	tw.Flush()
}

// PrintDiagnoseTable outputs a human-readable table for diagnose.DiagnoseResult.
func PrintDiagnoseTable(w io.Writer, res *diagnose.DiagnoseResult) {
	fmt.Fprintf(w, "COLLECTED: %s | WINDOW: %.0fs | TOTAL FINDINGS: %d\n\n",
		res.CollectedAt, res.WindowSeconds, res.TotalFindings)

	if res.TotalFindings == 0 {
		fmt.Fprintln(w, "No issues detected across monitored VMs and nodes.")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tRULE ID\tSUBJECT\tSUMMARY")
	fmt.Fprintln(tw, "--------\t-------\t-------\t-------")

	for _, f := range res.Findings {
		subjStr := f.Subject.Name
		if f.Subject.Kind == diagnose.SubjectKindVM && f.Subject.Namespace != "" {
			subjStr = fmt.Sprintf("%s/%s", f.Subject.Namespace, f.Subject.Name)
		}
		sevStr := strings.ToUpper(f.Severity)

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			sevStr,
			f.ID,
			subjStr,
			f.Summary,
		)
	}
	tw.Flush()
}
