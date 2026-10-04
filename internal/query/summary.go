package query

import (
	"math"
	"sort"
	"time"

	"github.com/coulof/kvtop/internal/store"
)

// FilterPointsInWindow returns Points with timestamps greater than or equal to cutoff.
func FilterPointsInWindow(points []store.Point, cutoff time.Time) []store.Point {
	if len(points) == 0 {
		return nil
	}
	var filtered []store.Point
	for _, p := range points {
		if !p.Timestamp.Before(cutoff) {
			filtered = append(filtered, p)
		}
	}
	// If cutoff is newer than all points but points exist, take at least the latest point
	// to avoid blanking stats if scraping was slightly delayed.
	if len(filtered) == 0 && len(points) > 0 {
		filtered = append(filtered, points[len(points)-1])
	}
	return filtered
}

// ComputeSummary calculates min, avg, max, p95, and last across points in the window.
func ComputeSummary(points []store.Point, cutoff time.Time) *MetricSummary {
	windowPts := FilterPointsInWindow(points, cutoff)
	if len(windowPts) == 0 {
		return nil
	}

	minVal := windowPts[0].Value
	maxVal := windowPts[0].Value
	sumVal := 0.0

	vals := make([]float64, len(windowPts))
	for i, p := range windowPts {
		vals[i] = p.Value
		if p.Value < minVal {
			minVal = p.Value
		}
		if p.Value > maxVal {
			maxVal = p.Value
		}
		sumVal += p.Value
	}

	avgVal := sumVal / float64(len(windowPts))
	lastVal := windowPts[len(windowPts)-1].Value

	// Calculate 95th percentile
	sort.Float64s(vals)
	rank := int(math.Ceil(0.95*float64(len(vals)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(vals) {
		rank = len(vals) - 1
	}
	p95Val := vals[rank]

	return &MetricSummary{
		Min:  minVal,
		Avg:  avgVal,
		Max:  maxVal,
		P95:  p95Val,
		Last: lastVal,
	}
}

// ComputeSaturationSummary scales a CPU summary by allotted vCPUs to produce saturation %.
func ComputeSaturationSummary(cpuSummary *MetricSummary, allottedCPUs int64) *MetricSummary {
	if cpuSummary == nil || allottedCPUs <= 0 {
		return nil
	}
	scale := 100.0 / float64(allottedCPUs)
	return &MetricSummary{
		Min:  cpuSummary.Min * scale,
		Avg:  cpuSummary.Avg * scale,
		Max:  cpuSummary.Max * scale,
		P95:  cpuSummary.P95 * scale,
		Last: cpuSummary.Last * scale,
	}
}

// ComputePercentSummary scales a memory usage summary by total bytes to produce used %.
func ComputePercentSummary(usedSummary *MetricSummary, totalBytes uint64) *MetricSummary {
	if usedSummary == nil || totalBytes == 0 {
		return nil
	}
	scale := 100.0 / float64(totalBytes)
	return &MetricSummary{
		Min:  usedSummary.Min * scale,
		Avg:  usedSummary.Avg * scale,
		Max:  usedSummary.Max * scale,
		P95:  usedSummary.P95 * scale,
		Last: usedSummary.Last * scale,
	}
}

// ConvertToTimestampedPoints converts store.Points to TimestampedPoints.
func ConvertToTimestampedPoints(points []store.Point, cutoff time.Time) []TimestampedPoint {
	windowPts := FilterPointsInWindow(points, cutoff)
	if len(windowPts) == 0 {
		return nil
	}
	res := make([]TimestampedPoint, len(windowPts))
	for i, p := range windowPts {
		res[i] = TimestampedPoint{
			Timestamp: p.Timestamp,
			Value:     p.Value,
		}
	}
	return res
}
