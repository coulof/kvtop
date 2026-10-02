package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FooterModel renders the interactive shortcut help line or search input prompt.
type FooterModel struct {
	IsFiltering bool
	FilterQuery string
	SortColumn  string
	SortReverse bool
	SortSat     bool
}

func (f FooterModel) View(width int) string {
	if f.IsFiltering {
		prompt := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("Search VM: ")
		query := lipgloss.NewStyle().Foreground(ColorWhite).Render(f.FilterQuery + "█")
		help := lipgloss.NewStyle().Foreground(ColorMuted).Render("  (Press Enter to apply, Esc to cancel)")
		return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(prompt + query + help)
	}

	keys := []struct {
		key  string
		desc string
	}{
		{"o", "name"},
		{"c", "cpu"},
		{"m", "mem"},
		{"n", "net"},
		{"d", "disk"},
		{"s", "sat"},
		{"r", "rev"},
		{"/", "filter"},
		{"tab", "nodes"},
		{"+/-", "interval"},
		{"?", "doc"},
		{"q", "quit"},
	}

	if width < 100 {
		keys = []struct {
			key  string
			desc string
		}{
			{"o", "name"},
			{"c", "cpu"},
			{"m", "mem"},
			{"/", "filter"},
			{"tab", "nodes"},
			{"?", "doc"},
			{"q", "quit"},
		}
	}

	var parts []string
	for _, k := range keys {
		part := KeyStyle.Render(k.key) + " " + DescStyle.Render(k.desc)
		parts = append(parts, part)
	}

	status := fmt.Sprintf("Sort: %s", strings.ToUpper(f.SortColumn))
	if f.SortReverse {
		status += " (rev)"
	}
	if f.SortColumn == "cpu" && f.SortSat {
		status += " [sat%]"
	}
	statusBadge := BadgeStyle.Render(status)

	line := strings.Join(parts, "  ") + "    " + statusBadge
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(line)
}
