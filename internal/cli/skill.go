package cli

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed assets/SKILL.md
var skillContent string

// RunSkill handles the `kvtop skill` subcommand, outputting an AI agent runbook.
func RunSkill(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("skill", flag.ContinueOnError)
	fs.SetOutput(stderr)

	outFile := fs.String("out", "", "File path where SKILL.md will be written (default: stdout)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		PrintJSONError(stdout, err)
		return 1
	}

	if *outFile == "" {
		_, _ = fmt.Fprint(stdout, skillContent)
		return 0
	}

	dir := filepath.Dir(*outFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			PrintJSONError(stdout, fmt.Errorf("failed creating directory %s: %w", dir, err))
			return 1
		}
	}

	if err := os.WriteFile(*outFile, []byte(skillContent), 0644); err != nil {
		PrintJSONError(stdout, fmt.Errorf("failed writing skill file: %w", err))
		return 1
	}

	fmt.Fprintf(stdout, "Skill successfully written to %s\n", *outFile)
	return 0
}
