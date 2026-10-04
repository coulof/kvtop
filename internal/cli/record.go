package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/coulof/kvtop/internal/record"
)

// RunRecord handles the `kvtop record` subcommand.
func RunRecord(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outputDir := fs.String("out", "", "Output directory where recording fixtures will be written (required)")
	duration := fs.Duration("duration", 0, "Duration for continuous recording (e.g. 10m, default: single snapshot)")
	anonymize := fs.Bool("anonymize", false, "Replace namespace, VM, node, and volume names with stable pseudonyms")

	kubeconfig := fs.String("kubeconfig", "", "Path to kubeconfig")
	replayDir := fs.String("replay", "", "Directory containing existing recorded scrape fixtures to re-record/anonymize")
	virtHandlerNS := fs.String("virt-handler-namespace", "harvester-system", "virt-handler namespace")
	interval := fs.Duration("interval", 2*time.Second, "Scrape polling interval")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		PrintJSONError(stdout, err)
		return 1
	}

	if *outputDir == "" {
		PrintJSONError(stdout, fmt.Errorf("--out <dir> is required"))
		return 1
	}

	opts := record.RecordOptions{
		OutputDir:            *outputDir,
		Duration:             *duration,
		Interval:             *interval,
		Anonymize:            *anonymize,
		ReplayDir:            *replayDir,
		Kubeconfig:           *kubeconfig,
		VirtHandlerNamespace: *virtHandlerNS,
	}

	if err := record.Record(ctx, opts); err != nil {
		PrintJSONError(stdout, err)
		return 1
	}

	fmt.Fprintf(stdout, "Recording successfully saved to %s\n", *outputDir)
	return 0
}
