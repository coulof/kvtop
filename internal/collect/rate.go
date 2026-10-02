package collect

// ComputeRate computes the rate of a floating point counter over a time delta dt (in seconds).
// If curr < prev or dt <= 0, it reports a reset or invalid delta and returns 0.
func ComputeRate(curr, prev float64, dt float64) (rate float64, reset bool) {
	if dt <= 0 || curr < prev {
		return 0, true
	}
	return (curr - prev) / dt, false
}

// ComputeUintRate computes the rate of a uint64 counter over a time delta dt (in seconds).
// If curr < prev or dt <= 0, it reports a reset or invalid delta and returns 0.
func ComputeUintRate(curr, prev uint64, dt float64) (rate float64, reset bool) {
	if dt <= 0 || curr < prev {
		return 0, true
	}
	return float64(curr-prev) / dt, false
}
