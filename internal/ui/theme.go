package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Base Colors
	ColorPrimary   = lipgloss.Color("#50fa7b") // Bright Green
	ColorSecondary = lipgloss.Color("#8be9fd") // Cyan
	ColorAccent    = lipgloss.Color("#bd93f9") // Purple
	ColorWarning   = lipgloss.Color("#ffb86c") // Orange
	ColorDanger    = lipgloss.Color("#ff5555") // Red
	ColorMuted     = lipgloss.Color("#6272a4") // Gray/Blue
	ColorDarkBg    = lipgloss.Color("#21222c") // Dark background
	ColorHighlight = lipgloss.Color("#44475a") // Selection highlight
	ColorWhite     = lipgloss.Color("#f8f8f2")

	// Box Styles
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(0, 1)

	PanelFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorAccent).
				Padding(0, 1)

	// Typography & Headers
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	HeaderLabelStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	HeaderValueStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Bold(true)

	BadgeStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Background(ColorHighlight).
			Padding(0, 1).
			Bold(true)

	BadgeActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#282a36")).
				Background(ColorPrimary).
				Padding(0, 1).
				Bold(true)

	// Table Styles
	TableHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorSecondary).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(ColorMuted)

	TableRowStyle = lipgloss.NewStyle().
			Foreground(ColorWhite)

	TableRowSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Background(ColorHighlight).
				Bold(true)

	KeyStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	DescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// ValueColor returns a green/yellow/red color based on a 0-100 percentage.
func ValueColor(pct float64) lipgloss.Color {
	switch {
	case pct >= 85.0:
		return ColorDanger
	case pct >= 60.0:
		return ColorWarning
	default:
		return ColorPrimary
	}
}

// RenderBar returns a compact colored text progress bar (e.g. [||||||....]).
func RenderBar(width int, pct float64) string {
	return RenderBarWithBg(width, pct, nil)
}

// RenderBarWithBg returns a compact colored text progress bar with an optional background color.
func RenderBarWithBg(width int, pct float64, bg lipgloss.TerminalColor) string {
	return RenderColoredBarWithBg(width, pct, ValueColor(pct), bg)
}

// RenderColoredBarWithBg returns a compact text progress bar with a specified bar color and optional background.
func RenderColoredBarWithBg(width int, pct float64, barColor, bg lipgloss.TerminalColor) string {
	if width < 3 {
		width = 3
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int((pct / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	barStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(ColorMuted)
	if bg != nil {
		barStyle = barStyle.Background(bg)
		emptyStyle = emptyStyle.Background(bg)
	}

	return barStyle.Render(strings.Repeat("■", filled)) + emptyStyle.Render(strings.Repeat("·", empty))
}

// FormatBytes formats byte counts into human readable strings.
func FormatBytes(b float64) string {
	const (
		kb = 1024.0
		mb = 1024.0 * kb
		gb = 1024.0 * mb
		tb = 1024.0 * gb
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2fT", b/tb)
	case b >= gb:
		return fmt.Sprintf("%.1fG", b/gb)
	case b >= mb:
		return fmt.Sprintf("%.1fM", b/mb)
	case b >= kb:
		return fmt.Sprintf("%.0fK", b/kb)
	default:
		return fmt.Sprintf("%.0fB", b)
	}
}

// FormatRate formats byte rates into human readable rate strings.
func FormatRate(r float64) string {
	return FormatBytes(r) + "/s"
}
