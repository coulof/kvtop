package diagnose

import (
	"time"
)

// Severity levels as specified in AGENTS.md.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// SubjectKind identifies the entity of a finding.
const (
	SubjectKindVM   = "VM"
	SubjectKindNode = "Node"
)

// Subject identifies the VM or physical Node evaluated by a rule.
type Subject struct {
	Kind      string `json:"kind"`                // "VM" or "Node"
	Name      string `json:"name"`                // Name of the resource
	Namespace string `json:"namespace,omitempty"` // Namespace (if Kind == "VM")
}

// Finding represents a deterministic issue detected by a diagnosis rule.
type Finding struct {
	ID            string                 `json:"id"`
	Severity      string                 `json:"severity"` // "info", "warning", "critical"
	Subject       Subject                `json:"subject"`
	Summary       string                 `json:"summary"`
	Evidence      map[string]interface{} `json:"evidence"`
	WindowSeconds float64                `json:"window_seconds"`
}

// DiagnoseOptions configures the scope and behavior of kvtop diagnose.
type DiagnoseOptions struct {
	TargetVM   string        // "namespace/name" or ""
	Namespaces []string      // Filter by namespaces
	Node       string        // Filter by node
	Window     time.Duration // Time window (default: 30s, min: 10s)
	Thresholds Thresholds    // Threshold overrides
}

// DiagnoseResult represents the root JSON schema contract for `kvtop diagnose`.
type DiagnoseResult struct {
	Schema        string    `json:"schema"`
	CollectedAt   string    `json:"collected_at"`
	WindowSeconds float64   `json:"window_seconds"`
	TotalFindings int       `json:"total_findings"`
	Findings      []Finding `json:"findings"`
}
