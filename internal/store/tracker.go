package store

import "time"

// FloatRateTracker tracks rate of change for a float64 counter over wall-clock time,
// handling virt-handler cache hits by holding sustained rate and decaying when idle.
type FloatRateTracker struct {
	lastValue float64
	lastTime  time.Time
	lastRate  float64
	hasBase   bool
}

// Reset clears the tracker baseline (used on VM migration or node moves).
func (tr *FloatRateTracker) Reset() {
	tr.hasBase = false
	tr.lastRate = 0
}

// Rate returns the current calculated rate.
func (tr *FloatRateTracker) Rate() float64 {
	return tr.lastRate
}

// Update processes a new counter sample and returns the current rate.
func (tr *FloatRateTracker) Update(val float64, t time.Time, cacheWindowSec float64) float64 {
	if !tr.hasBase {
		tr.lastValue = val
		tr.lastTime = t
		tr.hasBase = true
		tr.lastRate = 0
		return 0
	}

	if t.Before(tr.lastTime) {
		return tr.lastRate
	}

	if val < tr.lastValue {
		// Counter reset (reboot / migration)
		tr.lastValue = val
		tr.lastTime = t
		tr.lastRate = 0
		return 0
	}

	if val > tr.lastValue {
		dt := t.Sub(tr.lastTime).Seconds()
		if dt > 0 {
			tr.lastRate = (val - tr.lastValue) / dt
		}
		tr.lastValue = val
		tr.lastTime = t
		return tr.lastRate
	}

	// val == tr.lastValue (counter has not changed in virt-handler yet)
	elapsed := t.Sub(tr.lastTime).Seconds()
	if elapsed <= cacheWindowSec {
		// Cache hit: maintain last known active rate
		return tr.lastRate
	}

	// Beyond cache window: genuinely idle, decay to 0
	tr.lastRate = 0
	tr.lastTime = t // advance baseline while idle so resumed traffic computes accurately
	return 0
}

// UintRateTracker tracks rate of change for a uint64 counter over wall-clock time,
// handling virt-handler cache hits by holding sustained rate and decaying when idle.
type UintRateTracker struct {
	lastValue uint64
	lastTime  time.Time
	lastRate  float64
	hasBase   bool
}

// Reset clears the tracker baseline.
func (tr *UintRateTracker) Reset() {
	tr.hasBase = false
	tr.lastRate = 0
}

// Rate returns the current calculated rate.
func (tr *UintRateTracker) Rate() float64 {
	return tr.lastRate
}

// Update processes a new counter sample and returns the current rate.
func (tr *UintRateTracker) Update(val uint64, t time.Time, cacheWindowSec float64) float64 {
	if !tr.hasBase {
		tr.lastValue = val
		tr.lastTime = t
		tr.hasBase = true
		tr.lastRate = 0
		return 0
	}

	if t.Before(tr.lastTime) {
		return tr.lastRate
	}

	if val < tr.lastValue {
		// Counter reset (reboot / migration)
		tr.lastValue = val
		tr.lastTime = t
		tr.lastRate = 0
		return 0
	}

	if val > tr.lastValue {
		dt := t.Sub(tr.lastTime).Seconds()
		if dt > 0 {
			tr.lastRate = float64(val-tr.lastValue) / dt
		}
		tr.lastValue = val
		tr.lastTime = t
		return tr.lastRate
	}

	// val == tr.lastValue (counter has not changed in virt-handler yet)
	elapsed := t.Sub(tr.lastTime).Seconds()
	if elapsed <= cacheWindowSec {
		// Cache hit: maintain last known active rate
		return tr.lastRate
	}

	// Beyond cache window: genuinely idle, decay to 0
	tr.lastRate = 0
	tr.lastTime = t // advance baseline while idle so resumed traffic computes accurately
	return 0
}
