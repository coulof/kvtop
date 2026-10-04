package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/coulof/kvtop/internal/diagnose"
	"github.com/coulof/kvtop/internal/query"
)

// Server implements the Model Context Protocol (MCP) over stdio.
type Server struct {
	queryEngine *query.Engine
	diagnoser   *diagnose.Diagnoser
	allowExec   bool
	version     string
}

// NewServer creates a new MCP server.
func NewServer(queryEngine *query.Engine, diagnoser *diagnose.Diagnoser, allowExec bool, version string) *Server {
	if version == "" {
		version = "dev"
	}
	return &Server{
		queryEngine: queryEngine,
		diagnoser:   diagnoser,
		allowExec:   allowExec,
		version:     version,
	}
}

// Serve reads JSON-RPC requests from in and writes JSON-RPC responses to out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Support payloads up to 10MB
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		resp := s.HandleMessage(ctx, line)
		if resp != nil {
			respBytes, err := json.Marshal(resp)
			if err != nil {
				continue
			}
			respBytes = append(respBytes, '\n')
			if _, err := out.Write(respBytes); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

// HandleMessage processes a single JSON-RPC raw request and returns a response (or nil if notification).
func (s *Server) HandleMessage(ctx context.Context, raw []byte) *JSONRPCResponse {
	var req JSONRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    CodeParseError,
				Message: fmt.Sprintf("Parse error: %v", err),
			},
		}
	}

	// Notifications have no ID and do not expect a response
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		res := InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: map[string]interface{}{},
			},
			ServerInfo: ServerInfo{
				Name:    "kvtop",
				Version: s.version,
			},
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		}

	case "notifications/initialized":
		return nil

	case "ping":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ListToolsResult{
				Tools: s.getToolDefinitions(),
			},
		}

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInvalidParams,
					Message: fmt.Sprintf("Invalid params: %v", err),
				},
			}
		}

		result := s.executeTool(ctx, params.Name, params.Arguments)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}

	default:
		if isNotification {
			return nil
		}
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeMethodNotFound,
				Message: fmt.Sprintf("Method '%s' not found", req.Method),
			},
		}
	}
}

func (s *Server) getToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "top",
			Description: "List top Virtual Machines on the KubeVirt/Harvester cluster by resource usage (CPU, memory, network, disk IOPS) over a window.",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"sort": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"cpu", "mem", "net", "disk"},
						"description": "Metric to sort by (default: cpu)",
					},
					"by_saturation": map[string]interface{}{
						"type":        "boolean",
						"description": "Sort CPU by saturation percent (used/allotted) instead of absolute cores",
					},
					"namespaces": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Filter by namespace(s)",
					},
					"node": map[string]interface{}{
						"type":        "string",
						"description": "Filter by physical node name",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of VMs to return (default: 10)",
					},
					"window": map[string]interface{}{
						"type":        "string",
						"description": "Time window for rate calculation (e.g. '15s', '30s', '1m', default: '15s', minimum: '10s')",
					},
					"include_samples": map[string]interface{}{
						"type":        "boolean",
						"description": "Include raw time-series samples array",
					},
				},
			},
		},
		{
			Name:        "vm",
			Description: "Inspect a specific VirtualMachineInstance on the KubeVirt cluster including CPU topology, memory specs, conditions, attached volumes, and metric summaries.",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace of the VM (default: 'default')",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the VirtualMachineInstance",
					},
					"window": map[string]interface{}{
						"type":        "string",
						"description": "Time window for rate calculation (default: '15s', minimum: '10s')",
					},
					"include_samples": map[string]interface{}{
						"type":        "boolean",
						"description": "Include raw time-series samples array",
					},
					"allow_exec": map[string]interface{}{
						"type":        "boolean",
						"description": "Allow virsh exec drilldown into launcher pod (disabled unless server has --allow-exec)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "nodes",
			Description: "List physical cluster nodes with VM density, VM counts, allocatable capacity, overcommit ratio, and aggregated VM resource load.",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of nodes to return (0 for all)",
					},
					"window": map[string]interface{}{
						"type":        "string",
						"description": "Time window for calculation (default: '15s')",
					},
				},
			},
		},
		{
			Name:        "diagnose",
			Description: "Run deterministic health and performance diagnosis rules on VMs and nodes. Evaluates CPU saturation, memory pressure, disk latency, non-converging migrations, node imbalance, overcommit, and missing metrics.",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"target_vm": map[string]interface{}{
						"type":        "string",
						"description": "Target specific VM in 'namespace/name' format",
					},
					"namespaces": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Filter VMs by namespace",
					},
					"node": map[string]interface{}{
						"type":        "string",
						"description": "Filter by node name",
					},
					"window": map[string]interface{}{
						"type":        "string",
						"description": "Observation window for diagnosis (default: '30s', minimum: '10s')",
					},
					"threshold_cpu": map[string]interface{}{
						"type":        "number",
						"description": "Override CPU saturation ratio threshold (default: 0.90)",
					},
					"threshold_mem": map[string]interface{}{
						"type":        "number",
						"description": "Override memory pressure ratio threshold (default: 0.90)",
					},
					"threshold_overcommit": map[string]interface{}{
						"type":        "number",
						"description": "Override node memory overcommit ratio threshold (default: 1.50)",
					},
					"threshold_imbalance": map[string]interface{}{
						"type":        "number",
						"description": "Override node imbalance ratio threshold (default: 2.0)",
					},
				},
			},
		},
	}
}

func (s *Server) executeTool(ctx context.Context, name string, args map[string]interface{}) CallToolResult {
	if args == nil {
		args = make(map[string]interface{})
	}

	switch name {
	case "top":
		sortBy := "cpu"
		if v, ok := args["sort"].(string); ok && v != "" {
			sortBy = v
		}
		bySat, _ := args["by_saturation"].(bool)
		node, _ := args["node"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		window := parseDuration(args["window"], 15*time.Second)
		samples, _ := args["include_samples"].(bool)

		var namespaces []string
		if nsRaw, ok := args["namespaces"].([]interface{}); ok {
			for _, item := range nsRaw {
				if nsStr, ok := item.(string); ok && nsStr != "" {
					namespaces = append(namespaces, nsStr)
				}
			}
		}

		res, err := s.queryEngine.QueryTop(ctx, query.TopOptions{
			SortBy:         sortBy,
			BySaturation:   bySat,
			Namespaces:     namespaces,
			Node:           node,
			Limit:          limit,
			Window:         window,
			IncludeSamples: samples,
		})
		if err != nil {
			return formatErrorResult(err)
		}
		return formatSuccessResult(res)

	case "vm":
		nameStr, _ := args["name"].(string)
		if nameStr == "" {
			return formatErrorResult(fmt.Errorf("missing required parameter 'name'"))
		}
		nsStr, _ := args["namespace"].(string)
		if nsStr == "" {
			// Check if nameStr contains "ns/name"
			parts := strings.Split(nameStr, "/")
			if len(parts) == 2 {
				nsStr = parts[0]
				nameStr = parts[1]
			} else {
				nsStr = "default"
			}
		}
		window := parseDuration(args["window"], 15*time.Second)
		samples, _ := args["include_samples"].(bool)
		allowExec, _ := args["allow_exec"].(bool)

		res, err := s.queryEngine.QueryVM(ctx, nsStr, nameStr, query.VMOptions{
			Window:         window,
			IncludeSamples: samples,
			AllowExec:      allowExec && s.allowExec,
		})
		if err != nil {
			return formatErrorResult(err)
		}
		return formatSuccessResult(res)

	case "nodes":
		limit := 0
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		window := parseDuration(args["window"], 15*time.Second)

		res, err := s.queryEngine.QueryNodes(ctx, query.NodesOptions{
			Limit:  limit,
			Window: window,
		})
		if err != nil {
			return formatErrorResult(err)
		}
		return formatSuccessResult(res)

	case "diagnose":
		targetVM, _ := args["target_vm"].(string)
		node, _ := args["node"].(string)
		window := parseDuration(args["window"], 30*time.Second)

		var namespaces []string
		if nsRaw, ok := args["namespaces"].([]interface{}); ok {
			for _, item := range nsRaw {
				if nsStr, ok := item.(string); ok && nsStr != "" {
					namespaces = append(namespaces, nsStr)
				}
			}
		}

		th := diagnose.DefaultThresholds()
		if v, ok := args["threshold_cpu"].(float64); ok && v > 0 {
			th.CPUSaturationRatio = v
		}
		if v, ok := args["threshold_mem"].(float64); ok && v > 0 {
			th.MemGuestPressureRatio = v
		}
		if v, ok := args["threshold_overcommit"].(float64); ok && v > 0 {
			th.NodeOvercommitRatio = v
		}
		if v, ok := args["threshold_imbalance"].(float64); ok && v > 0 {
			th.NodeImbalanceRatio = v
		}

		res, err := s.diagnoser.Diagnose(ctx, diagnose.DiagnoseOptions{
			TargetVM:   targetVM,
			Namespaces: namespaces,
			Node:       node,
			Window:     window,
			Thresholds: th,
		})
		if err != nil {
			return formatErrorResult(err)
		}
		return formatSuccessResult(res)

	default:
		return formatErrorResult(fmt.Errorf("unknown tool: %s", name))
	}
}

func formatSuccessResult(data interface{}) CallToolResult {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return formatErrorResult(err)
	}
	return CallToolResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}
}

func formatErrorResult(err error) CallToolResult {
	errPayload := map[string]interface{}{
		"schema": query.SchemaVersion,
		"error":  err.Error(),
	}
	jsonBytes, _ := json.MarshalIndent(errPayload, "", "  ")
	return CallToolResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
		IsError: true,
	}
}

func parseDuration(val interface{}, defaultVal time.Duration) time.Duration {
	if s, ok := val.(string); ok && s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	if f, ok := val.(float64); ok && f > 0 {
		return time.Duration(f * float64(time.Second))
	}
	return defaultVal
}
