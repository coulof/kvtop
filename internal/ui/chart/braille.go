package chart

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BrailleCanvas provides a 2D high-resolution pixel canvas backed by Unicode braille characters.
// Each character represents a 2x4 pixel dot matrix.
type BrailleCanvas struct {
	charWidth  int
	charHeight int
	pixelWidth int
	pixelHeight int
	grid       [][]rune
}

// NewBrailleCanvas initializes a canvas of specified character dimensions.
func NewBrailleCanvas(w, h int) *BrailleCanvas {
	if w <= 0 {
		w = 10
	}
	if h <= 0 {
		h = 3
	}

	grid := make([][]rune, h)
	for r := 0; r < h; r++ {
		grid[r] = make([]rune, w)
		for c := 0; c < w; c++ {
			grid[r][c] = 0x2800 // blank braille
		}
	}

	return &BrailleCanvas{
		charWidth:   w,
		charHeight:  h,
		pixelWidth:  w * 2,
		pixelHeight: h * 4,
		grid:        grid,
	}
}

// SetPixel turns on a pixel at (x, y) where (0,0) is bottom-left.
func (b *BrailleCanvas) SetPixel(x, y int) {
	if x < 0 || x >= b.pixelWidth || y < 0 || y >= b.pixelHeight {
		return
	}

	charCol := x / 2
	// y=0 is bottom, so row 0 in grid is top: invert y
	invY := (b.pixelHeight - 1) - y
	charRow := invY / 4

	dx := x % 2
	dy := invY % 4

	var dotMask rune
	if dx == 0 {
		// Left column: dot 1 (top), 2, 3, 7 (bottom)
		switch dy {
		case 0:
			dotMask = 0x01
		case 1:
			dotMask = 0x02
		case 2:
			dotMask = 0x04
		case 3:
			dotMask = 0x40
		}
	} else {
		// Right column: dot 4 (top), 5, 6, 8 (bottom)
		switch dy {
		case 0:
			dotMask = 0x08
		case 1:
			dotMask = 0x10
		case 2:
			dotMask = 0x20
		case 3:
			dotMask = 0x80
		}
	}

	b.grid[charRow][charCol] |= dotMask
}

// DrawLine draws a connected line between (x0, y0) and (x1, y1) using Bresenham's algorithm.
func (b *BrailleCanvas) DrawLine(x0, y0, x1, y1 int) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy

	for {
		b.SetPixel(x0, y0)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// DrawThresholdLine draws a dashed horizontal guideline across the canvas at the given ratio (0.0 to 1.0).
func (b *BrailleCanvas) DrawThresholdLine(ratio float64) {
	if ratio <= 0 || ratio > 1.0 {
		return
	}
	y := int(ratio * float64(b.pixelHeight-1))
	for x := 0; x < b.pixelWidth; x++ {
		// Dashed line: set 2 pixels on, 2 pixels off
		if (x/2)%2 == 0 {
			b.SetPixel(x, y)
		}
	}
}

// Render returns the colored multi-line braille graph string with a vertical gradient.
func (b *BrailleCanvas) Render(baseColor lipgloss.Color) string {
	var lines []string

	for r := 0; r < b.charHeight; r++ {
		var rowSb strings.Builder

		// Determine gradient color based on row height (top is higher/warmer)
		rowRatio := float64(b.charHeight-1-r) / float64(max(1, b.charHeight-1))
		rowColor := gradientColor(rowRatio, baseColor)
		style := lipgloss.NewStyle().Foreground(rowColor)

		for c := 0; c < b.charWidth; c++ {
			rn := b.grid[r][c]
			if rn == 0x2800 {
				rowSb.WriteString(" ")
			} else {
				rowSb.WriteString(style.Render(string(rn)))
			}
		}
		lines = append(lines, rowSb.String())
	}

	return strings.Join(lines, "\n")
}

// RenderBrailleGraph plots time-series values as a 2D line graph.
func RenderBrailleGraph(width, height int, values []float64, maxVal float64, baseColor lipgloss.Color) string {
	return RenderBrailleGraphWithThreshold(width, height, values, maxVal, 0, baseColor)
}

// RenderBrailleGraphWithThreshold plots time-series values with an optional horizontal threshold guideline (e.g. 0.90 for 90%).
func RenderBrailleGraphWithThreshold(width, height int, values []float64, maxVal float64, thresholdRatio float64, baseColor lipgloss.Color) string {
	if width <= 2 || height <= 1 {
		return ""
	}

	canvas := NewBrailleCanvas(width, height)
	pw := canvas.pixelWidth
	ph := canvas.pixelHeight

	// Draw threshold guideline if specified
	if thresholdRatio > 0 && thresholdRatio <= 1.0 {
		canvas.DrawThresholdLine(thresholdRatio)
	}

	if maxVal <= 0 {
		for _, v := range values {
			if v > maxVal {
				maxVal = v
			}
		}
		if maxVal <= 0 {
			maxVal = 1.0
		}
	}

	// Prepare data points mapped to pixel width (right-aligned)
	numPts := len(values)
	if numPts == 0 {
		return canvas.Render(baseColor)
	}

	start := 0
	if numPts > pw {
		start = numPts - pw
	}
	slice := values[start:]
	offset := pw - len(slice)

	prevX := -1
	prevY := -1

	for i, v := range slice {
		x := offset + i
		norm := v / maxVal
		if norm > 1.0 {
			norm = 1.0
		}
		if norm < 0.0 {
			norm = 0.0
		}
		y := int(norm * float64(ph-1))

		if prevX >= 0 {
			canvas.DrawLine(prevX, prevY, x, y)
		} else {
			canvas.SetPixel(x, y)
		}

		prevX = x
		prevY = y
	}

	return canvas.Render(baseColor)
}

func gradientColor(ratio float64, fallback lipgloss.Color) lipgloss.Color {
	switch {
	case ratio >= 0.80:
		return colorRed
	case ratio >= 0.55:
		return colorOrange
	case ratio >= 0.30:
		return colorYellow
	default:
		return colorGreen
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
