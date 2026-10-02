package store

import "time"

// Point represents a timestamped metric sample in a time series.
type Point struct {
	Timestamp time.Time
	Value     float64
}

// RingBuffer stores a fixed-capacity circular buffer of Points.
// It is optimized for zero allocations during steady-state Push operations.
type RingBuffer struct {
	capacity int
	points   []Point
	head     int // Index of the oldest element when count == capacity
	count    int // Number of elements currently stored
}

// NewRingBuffer creates a ring buffer with the specified capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 300
	}
	return &RingBuffer{
		capacity: capacity,
		points:   make([]Point, capacity),
	}
}

// Push adds a new point to the buffer, evicting the oldest point if full.
func (r *RingBuffer) Push(t time.Time, val float64) {
	if r.count < r.capacity {
		r.points[r.count] = Point{Timestamp: t, Value: val}
		r.count++
		return
	}

	// Full: overwrite oldest at head and advance head
	r.points[r.head] = Point{Timestamp: t, Value: val}
	r.head = (r.head + 1) % r.capacity
}

// Len returns the current number of points stored.
func (r *RingBuffer) Len() int {
	return r.count
}

// Capacity returns the maximum capacity of the ring buffer.
func (r *RingBuffer) Capacity() int {
	return r.capacity
}

// Latest returns the most recently pushed point, or false if empty.
func (r *RingBuffer) Latest() (Point, bool) {
	if r.count == 0 {
		return Point{}, false
	}
	if r.count < r.capacity {
		return r.points[r.count-1], true
	}
	// When full, latest is at (head - 1 + capacity) % capacity
	idx := (r.head - 1 + r.capacity) % r.capacity
	return r.points[idx], true
}

// Points returns all points in chronological order (oldest to newest).
func (r *RingBuffer) Points() []Point {
	if r.count == 0 {
		return nil
	}

	res := make([]Point, r.count)
	if r.count < r.capacity {
		copy(res, r.points[:r.count])
		return res
	}

	// Two slices: [head:] followed by [:head]
	n := copy(res, r.points[r.head:])
	copy(res[n:], r.points[:r.head])
	return res
}

// Values returns all point values in chronological order (oldest to newest).
func (r *RingBuffer) Values() []float64 {
	if r.count == 0 {
		return nil
	}

	res := make([]float64, r.count)
	if r.count < r.capacity {
		for i := 0; i < r.count; i++ {
			res[i] = r.points[i].Value
		}
		return res
	}

	idx := 0
	for i := r.head; i < r.capacity; i++ {
		res[idx] = r.points[i].Value
		idx++
	}
	for i := 0; i < r.head; i++ {
		res[idx] = r.points[i].Value
		idx++
	}
	return res
}

// Clear resets the ring buffer to empty.
func (r *RingBuffer) Clear() {
	r.head = 0
	r.count = 0
}
