package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coulof/kvtop/internal/cli"
	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/query"
)

func TestCLITopJSON(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"top",
		"--replay", testdataDir,
		"-n", "2",
		"--window", "15s",
		"-o", "json",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s, stdout: %s", exitCode, stderr.String(), stdout.String())
	}

	var res query.TopResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON stdout: %v\nOutput was: %s", err, stdout.String())
	}

	if res.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, res.Schema)
	}
	if res.WindowSeconds != 15 {
		t.Errorf("expected window_seconds 15, got %f", res.WindowSeconds)
	}
	if len(res.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(res.Items))
	}
	if !res.Truncated {
		t.Errorf("expected truncated to be true")
	}
}

func TestCLITopTable(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"top",
		"--replay", testdataDir,
		"-o", "table",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	out := stdout.String()
	if !strings.Contains(out, "NAMESPACE") || !strings.Contains(out, "CPU (AVG/LAST/ALLOT)") {
		t.Errorf("expected table header in stdout, got:\n%s", out)
	}
}

func TestCLITopWindowValidation(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"top",
		"--replay", testdataDir,
		"--window", "5s",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for window < 10s")
	}

	var errResp cli.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("expected valid JSON error response: %v\nOutput: %s", err, stdout.String())
	}
	if !strings.Contains(errResp.Error, "10s") {
		t.Errorf("expected error mentioning 10s, got: %s", errResp.Error)
	}
}

func TestCLIVM(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"vm",
		"default/coriolis-win-minion",
		"--replay", testdataDir,
		"-o", "json",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stdout: %s", exitCode, stdout.String())
	}

	var res query.VMResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON stdout: %v\nOutput was: %s", err, stdout.String())
	}

	if res.VM.Name != "coriolis-win-minion" {
		t.Errorf("expected vm name coriolis-win-minion, got %s", res.VM.Name)
	}
	if res.VM.Context == nil {
		t.Fatalf("expected context populated on VM")
	}
	if res.VM.Context.CPUTopology.Cores != 4 {
		t.Errorf("expected 4 vCPUs in topology, got %d", res.VM.Context.CPUTopology.Cores)
	}
}

func TestCLIVMNotFound(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"vm",
		"default/non-existent-vm",
		"--replay", testdataDir,
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for non-existent VM")
	}

	var errResp cli.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON error on stdout: %v", err)
	}
	if !strings.Contains(errResp.Error, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", errResp.Error)
	}
}

func TestCLINodes(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"nodes",
		"--replay", testdataDir,
		"-o", "json",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	var res query.NodesResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if res.Total != 4 {
		t.Errorf("expected 4 nodes, got %d", res.Total)
	}
	if len(res.Items) != 4 {
		t.Errorf("expected 4 items, got %d", len(res.Items))
	}
	if res.Items[0].CPUAllocatableCores != 16 {
		t.Errorf("expected 16 allocatable cores, got %d", res.Items[0].CPUAllocatableCores)
	}

	// Test with --limit alias
	stdoutLimit := &bytes.Buffer{}
	exitCodeLimit := cli.Run(context.Background(), []string{"nodes", "--replay", testdataDir, "--limit", "2"}, stdoutLimit, stderr)
	if exitCodeLimit != 0 {
		t.Fatalf("expected exit code 0 for --limit, got %d", exitCodeLimit)
	}
	var resLimit query.NodesResult
	if err := json.Unmarshal(stdoutLimit.Bytes(), &resLimit); err != nil {
		t.Fatalf("failed to parse JSON from --limit: %v", err)
	}
	if len(resLimit.Items) != 2 || !resLimit.Truncated {
		t.Errorf("expected 2 items and truncated: true with --limit 2, got %d items (truncated: %v)", len(resLimit.Items), resLimit.Truncated)
	}
}

func TestCLIDiagnoseJSON(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"diagnose",
		"--replay", testdataDir,
		"-o", "json",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stdout: %s", exitCode, stdout.String())
	}

	var res diagnose.DiagnoseResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON from diagnose: %v", err)
	}

	if res.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, res.Schema)
	}
	if res.TotalFindings == 0 {
		t.Errorf("expected findings in replay cluster")
	}
}

func TestCLIDiagnoseTable(t *testing.T) {
	testdataDir := filepath.Join("..", "..", "testdata")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	args := []string{
		"diagnose",
		"--replay", testdataDir,
		"-o", "table",
	}

	exitCode := cli.Run(context.Background(), args, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	out := stdout.String()
	if !strings.Contains(out, "SEVERITY") || !strings.Contains(out, "RULE ID") {
		t.Errorf("expected table header in stdout, got:\n%s", out)
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := cli.Run(context.Background(), []string{"unknown-cmd"}, stdout, stderr)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code")
	}

	var errResp cli.ErrorResponse
	if err := json.Unmarshal(stdout.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON error: %v", err)
	}
	if !strings.Contains(errResp.Error, "unknown subcommand") {
		t.Errorf("expected unknown subcommand message, got: %s", errResp.Error)
	}
}
