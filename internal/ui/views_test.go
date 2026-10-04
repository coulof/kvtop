package ui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coulof/kvtop/internal/query"
	"github.com/coulof/kvtop/internal/store"
	"github.com/coulof/kvtop/internal/ui"
)

func TestViews_Rendering(t *testing.T) {
	st := store.NewStore(300)
	st.SetNodeAllocatable("hv-01", 64*1024*1024*1024)
	st.SetNodeAllocatable("hv-02", 64*1024*1024*1024)

	engine := query.NewEngine(st, nil, "")
	app := ui.NewAppModel(engine, "test-cluster", "v1.36.3", 2*time.Second)

	// Simulate window resize wide (120 cols)
	modelWide, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	viewWide := modelWide.View()
	wideHeight := lipgloss.Height(viewWide)
	if wideHeight > 30 {
		t.Errorf("expected viewWide height <= 30, got %d", wideHeight)
	}

	// Verify aggregate panel titles and numbers are present
	if !strings.Contains(viewWide, "Total VM CPU") {
		t.Errorf("expected viewWide to contain 'Total VM CPU'")
	}
	if !strings.Contains(viewWide, "cores") {
		t.Errorf("expected viewWide to contain CPU numbers/cores")
	}
	if !strings.Contains(viewWide, "Total VM Memory (Guest)") {
		t.Errorf("expected viewWide to contain 'Total VM Memory (Guest)'")
	}
	if !strings.Contains(viewWide, "Total VM Network (Aggregate)") {
		t.Errorf("expected viewWide to contain 'Total VM Network (Aggregate)'")
	}

	// Verify Nodes table header is present
	if !strings.Contains(viewWide, "HOST") || !strings.Contains(viewWide, "MEM (OVERCOMMIT)") {
		t.Errorf("expected viewWide to contain Nodes table headers")
	}

	// Simulate window resize narrow (80 cols < 100 breakpoint)
	modelNarrow, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	viewNarrow := modelNarrow.View()
	narrowHeight := lipgloss.Height(viewNarrow)
	if narrowHeight > 24 {
		t.Errorf("expected viewNarrow height <= 24, got %d", narrowHeight)
	}
}

func TestNodesView_InlineBar(t *testing.T) {
	nm := ui.NodesModel{}
	nodes := []store.NodeAggregate{
		{
			NodeName:        "hv-01",
			VMCount:         0,
			OvercommitRatio: 0.1,
		},
		{
			NodeName:        "hv-02",
			VMCount:         4,
			OvercommitRatio: 0.5,
		},
	}
	rendered := nm.View(80, 8, nodes)
	if !strings.Contains(rendered, "0VM") || !strings.Contains(rendered, "4VM") {
		t.Errorf("expected rendered nodes to contain VM counts, got: %s", rendered)
	}
	// Verify horizontal bar symbols exist
	if !strings.Contains(rendered, "■") && !strings.Contains(rendered, "·") {
		t.Errorf("expected horizontal bar characters in nodes view, got: %s", rendered)
	}
}
