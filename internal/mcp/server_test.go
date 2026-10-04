package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/collect"
	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/kube"
	"github.com/coulof/kvtop/internal/mcp"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
)

func setupTestMCPServer(t *testing.T) *mcp.Server {
	st := store.NewStore(300)
	testdataDir := filepath.Join("..", "..", "testdata")

	vmis, err := collect.LoadReplayVMIs(testdataDir)
	if err != nil {
		t.Fatalf("failed loading replay VMIs: %v", err)
	}
	st.UpdateVMIs(vmis)

	for _, n := range []string{"hv-01", "hv-02", "hv-03", "hv-04"} {
		st.OnNodeUpdated(kube.NodeInfo{
			Name:            n,
			AllocatableMem:  96 * 1024 * 1024 * 1024,
			AllocatableCPUs: 16,
			Ready:           true,
		})
	}

	rc, err := collect.NewReplayCollector(testdataDir, 2*time.Second)
	if err != nil {
		t.Fatalf("failed creating replay collector: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		samples, err := rc.Collect(ctx)
		if err != nil {
			t.Fatalf("failed collecting replay samples: %v", err)
		}
		st.PushSamples(samples)
	}

	engine := query.NewEngine(st, nil, testdataDir)
	diagnoser := diagnose.NewDiagnoser(engine)
	return mcp.NewServer(engine, diagnoser, false, "v0.1.0-test")
}

func TestMCPInitialize(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2024-11-05"}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	initRes, ok := resp.Result.(mcp.InitializeResult)
	if !ok {
		t.Fatalf("expected InitializeResult, got %T", resp.Result)
	}
	if initRes.ProtocolVersion != mcp.ProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", mcp.ProtocolVersion, initRes.ProtocolVersion)
	}
	if initRes.ServerInfo.Name != "kvtop" {
		t.Errorf("expected server name kvtop, got %s", initRes.ServerInfo.Name)
	}
}

func TestMCPNotifications(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "method": "notifications/initialized"}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp != nil {
		t.Errorf("expected nil response for notification, got %v", resp)
	}
}

func TestMCPPing(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 2, "method": "ping"}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected successful ping response")
	}
}

func TestMCPToolsList(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 3, "method": "tools/list"}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("expected successful tools/list response: %v", resp)
	}

	listRes, ok := resp.Result.(mcp.ListToolsResult)
	if !ok {
		t.Fatalf("expected ListToolsResult, got %T", resp.Result)
	}
	if len(listRes.Tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(listRes.Tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range listRes.Tools {
		toolNames[tool.Name] = true
	}
	for _, expected := range []string{"top", "vm", "nodes", "diagnose"} {
		if !toolNames[expected] {
			t.Errorf("tool '%s' not listed in tools/list", expected)
		}
	}
}

func TestMCPToolsCall_Top(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": {"name": "top", "arguments": {"limit": 2, "sort": "cpu"}}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call top failed: %v", resp)
	}

	callRes, ok := resp.Result.(mcp.CallToolResult)
	if !ok || len(callRes.Content) == 0 {
		t.Fatalf("expected non-empty CallToolResult")
	}

	var topRes query.TopResult
	if err := json.Unmarshal([]byte(callRes.Content[0].Text), &topRes); err != nil {
		t.Fatalf("failed unmarshaling top result text: %v", err)
	}
	if topRes.Schema != query.SchemaVersion {
		t.Errorf("expected schema %s, got %s", query.SchemaVersion, topRes.Schema)
	}
	if len(topRes.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(topRes.Items))
	}
}

func TestMCPToolsCall_VM(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": {"name": "vm", "arguments": {"namespace": "default", "name": "coriolis-win-minion"}}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call vm failed: %v", resp)
	}

	callRes := resp.Result.(mcp.CallToolResult)
	var vmRes query.VMResult
	if err := json.Unmarshal([]byte(callRes.Content[0].Text), &vmRes); err != nil {
		t.Fatalf("failed unmarshaling vm result text: %v", err)
	}
	if vmRes.VM.Name != "coriolis-win-minion" {
		t.Errorf("expected coriolis-win-minion, got %s", vmRes.VM.Name)
	}
}

func TestMCPToolsCall_Nodes(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 6, "method": "tools/call", "params": {"name": "nodes", "arguments": {}}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call nodes failed: %v", resp)
	}

	callRes := resp.Result.(mcp.CallToolResult)
	var nodesRes query.NodesResult
	if err := json.Unmarshal([]byte(callRes.Content[0].Text), &nodesRes); err != nil {
		t.Fatalf("failed unmarshaling nodes result text: %v", err)
	}
	if nodesRes.Total != 4 {
		t.Errorf("expected 4 nodes, got %d", nodesRes.Total)
	}
}

func TestMCPToolsCall_Diagnose(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 7, "method": "tools/call", "params": {"name": "diagnose", "arguments": {"window": "30s"}}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call diagnose failed: %v", resp)
	}

	callRes := resp.Result.(mcp.CallToolResult)
	var diagRes diagnose.DiagnoseResult
	if err := json.Unmarshal([]byte(callRes.Content[0].Text), &diagRes); err != nil {
		t.Fatalf("failed unmarshaling diagnose result text: %v", err)
	}
	if diagRes.TotalFindings == 0 {
		t.Errorf("expected diagnose findings")
	}
}

func TestMCPToolsCall_UnknownTool(t *testing.T) {
	srv := setupTestMCPServer(t)
	raw := []byte(`{"jsonrpc": "2.0", "id": 8, "method": "tools/call", "params": {"name": "non_existent_tool"}}`)

	resp := srv.HandleMessage(context.Background(), raw)
	if resp == nil {
		t.Fatalf("expected response")
	}
	callRes := resp.Result.(mcp.CallToolResult)
	if !callRes.IsError {
		t.Errorf("expected IsError true for unknown tool")
	}
}

func TestMCPServeStream(t *testing.T) {
	srv := setupTestMCPServer(t)

	inBuf := &bytes.Buffer{}
	outBuf := &bytes.Buffer{}

	inBuf.WriteString(`{"jsonrpc": "2.0", "id": 10, "method": "ping"}` + "\n")
	inBuf.WriteString(`{"jsonrpc": "2.0", "id": 11, "method": "tools/list"}` + "\n")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Serve(ctx, inBuf, outBuf)
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d:\n%s", len(lines), outBuf.String())
	}
}
