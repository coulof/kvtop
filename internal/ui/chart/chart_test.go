package chart_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/ui/chart"
)

func TestRenderBrailleSparkline_WidthAndValues(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		maxVal   float64
		width    int
		expected int
	}{
		{
			name:     "empty values",
			values:   nil,
			maxVal:   1.0,
			width:    6,
			expected: 6,
		},
		{
			name:     "partial data",
			values:   []float64{0.2, 0.5, 0.8},
			maxVal:   1.0,
			width:    6,
			expected: 6,
		},
		{
			name:     "overflow data (more than 2*width points)",
			values:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 1.1, 1.2, 1.3, 1.4, 1.5},
			maxVal:   2.0,
			width:    5,
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := chart.RenderBrailleSparkline(tt.values, tt.maxVal, tt.width)
			visibleLen := lipgloss.Width(rendered)
			if visibleLen != tt.expected {
				t.Errorf("expected visible length %d, got %d (rendered: %q)", tt.expected, visibleLen, rendered)
			}
		})
	}
}

func TestBrailleCanvas_Drawing(t *testing.T) {
	canvas := chart.NewBrailleCanvas(10, 4)
	if canvas == nil {
		t.Fatalf("expected non-nil canvas")
	}

	// Test bounds safety (negative or out-of-range should not panic)
	canvas.SetPixel(-1, -1)
	canvas.SetPixel(100, 100)
	canvas.DrawLine(-5, -5, 50, 50)

	// Draw diagonal line
	canvas.DrawLine(0, 0, 19, 15)

	rendered := canvas.Render(lipgloss.Color("#50fa7b"))
	lines := strings.Split(rendered, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines in rendered canvas, got %d", len(lines))
	}

	for i, l := range lines {
		w := lipgloss.Width(l)
		if w != 10 {
			t.Errorf("line %d: expected width 10, got %d", i, w)
		}
	}
}

func TestRenderBrailleGraph_TimeSeries(t *testing.T) {
	data := []float64{1.0, 2.0, 3.5, 4.0, 2.5, 1.8, 0.5, 1.2, 2.8, 3.9}
	width := 20
	height := 5

	graph := chart.RenderBrailleGraph(width, height, data, 4.0, lipgloss.Color("#50fa7b"))
	lines := strings.Split(graph, "\n")
	if len(lines) != height {
		t.Fatalf("expected %d lines in rendered graph, got %d", height, len(lines))
	}

	for i, l := range lines {
		w := lipgloss.Width(l)
		if w != width {
			t.Errorf("line %d: expected width %d, got %d", i, width, w)
		}
	}
}
