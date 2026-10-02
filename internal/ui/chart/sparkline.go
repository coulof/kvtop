package chart

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorGreen  = lipgloss.Color("#50fa7b")
	colorYellow = lipgloss.Color("#f1fa8c")
	colorOrange = lipgloss.Color("#ffb86c")
	colorRed    = lipgloss.Color("#ff5555")
	colorDim    = lipgloss.Color("#44475a")
)

// Braille column dot masks (from bottom to top)
var (
	// Left column: dot 7, 3, 2, 1
	leftDots = []rune{
		0,      // 0 dots
		0x0040, // 1 dot (bottom: dot 7)
		0x0044, // 2 dots (dots 7, 3)
		0x0046, // 3 dots (dots 7, 3, 2)
		0x0047, // 4 dots (dots 7, 3, 2, 1)
	}

	// Right column: dot 8, 6, 5, 4
	rightDots = []rune{
		0,      // 0 dots
		0x0080, // 1 dot (bottom: dot 8)
		0x00A0, // 2 dots (dots 8, 6)
		0x00B0, // 3 dots (dots 8, 6, 5)
		0x00B8, // 4 dots (dots 8, 6, 5, 4)
	}
)

// RenderBrailleSparkline renders a high-density 1-line braille sparkline of exact character width.
func RenderBrailleSparkline(values []float64, maxVal float64, width int) string {
	return RenderBrailleSparklineWithBg(values, maxVal, width, nil)
}

// RenderBrailleSparklineWithBg renders a high-density 1-line braille sparkline with an optional background color.
func RenderBrailleSparklineWithBg(values []float64, maxVal float64, width int, bg lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}

	totalPoints := width * 2
	points := make([]float64, totalPoints)

	// Fill right-aligned (newest data on right)
	if len(values) > 0 {
		start := 0
		if len(values) > totalPoints {
			start = len(values) - totalPoints
		}
		slice := values[start:]
		offset := totalPoints - len(slice)
		for i, v := range slice {
			points[offset+i] = v
		}
	}

	// Determine maxVal if not provided or zero
	if maxVal <= 0 {
		for _, v := range points {
			if v > maxVal {
				maxVal = v
			}
		}
		if maxVal <= 0 {
			maxVal = 1.0
		}
	}

	var sb strings.Builder

	for col := 0; col < width; col++ {
		vLeft := points[col*2]
		vRight := points[col*2+1]

		lvlLeft := valToLevel(vLeft, maxVal)
		lvlRight := valToLevel(vRight, maxVal)

		r := rune(0x2800) | leftDots[lvlLeft] | rightDots[lvlRight]
		if r == 0x2800 {
			// Subtly show an empty baseline dot or empty cell
			r = '⣀' // low baseline dots so it doesn't look blank
		}

		// Color based on highest point in this column
		maxV := vLeft
		if vRight > maxV {
			maxV = vRight
		}
		pct := (maxV / maxVal) * 100.0
		color := sparklineColor(pct)

		st := lipgloss.NewStyle().Foreground(color)
		if bg != nil {
			st = st.Background(bg)
		}
		sb.WriteString(st.SetString(string(r)).String())
	}

	return sb.String()
}

func valToLevel(val, max float64) int {
	if val <= 0 || max <= 0 {
		return 0
	}
	ratio := val / max
	if ratio >= 0.85 {
		return 4
	}
	if ratio >= 0.55 {
		return 3
	}
	if ratio >= 0.25 {
		return 2
	}
	if ratio > 0.02 {
		return 1
	}
	return 0
}

func sparklineColor(pct float64) lipgloss.Color {
	switch {
	case pct >= 85.0:
		return colorRed
	case pct >= 65.0:
		return colorOrange
	case pct >= 40.0:
		return colorYellow
	default:
		return colorGreen
	}
}
