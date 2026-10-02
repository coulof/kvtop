package store_test

import (
	"testing"
	"time"

	"github.com/coulof/kvtop/internal/store"
)

func TestRingBuffer_Basic(t *testing.T) {
	rb := store.NewRingBuffer(3)
	if rb.Capacity() != 3 {
		t.Fatalf("expected capacity 3, got %d", rb.Capacity())
	}
	if rb.Len() != 0 {
		t.Fatalf("expected initial len 0, got %d", rb.Len())
	}

	_, ok := rb.Latest()
	if ok {
		t.Fatalf("expected Latest() to be false on empty buffer")
	}

	now := time.Now()
	rb.Push(now, 10.0)
	rb.Push(now.Add(time.Second), 20.0)

	if rb.Len() != 2 {
		t.Fatalf("expected len 2, got %d", rb.Len())
	}

	latest, ok := rb.Latest()
	if !ok || latest.Value != 20.0 {
		t.Fatalf("expected latest value 20.0, got %v", latest)
	}

	vals := rb.Values()
	if len(vals) != 2 || vals[0] != 10.0 || vals[1] != 20.0 {
		t.Fatalf("expected [10, 20], got %v", vals)
	}
}

func TestRingBuffer_EvictionAndOrder(t *testing.T) {
	rb := store.NewRingBuffer(3)
	t0 := time.Now()

	// Push 5 items into capacity 3: 1, 2, 3, 4, 5
	// After 1, 2, 3: [1, 2, 3]
	// After 4: evicts 1 -> [2, 3, 4]
	// After 5: evicts 2 -> [3, 4, 5]
	for i := 1; i <= 5; i++ {
		rb.Push(t0.Add(time.Duration(i)*time.Second), float64(i))
	}

	if rb.Len() != 3 {
		t.Fatalf("expected len 3, got %d", rb.Len())
	}

	latest, ok := rb.Latest()
	if !ok || latest.Value != 5.0 {
		t.Fatalf("expected latest value 5.0, got %v", latest)
	}

	vals := rb.Values()
	expected := []float64{3.0, 4.0, 5.0}
	for i, v := range expected {
		if vals[i] != v {
			t.Errorf("at index %d: expected %f, got %f (full: %v)", i, v, vals[i], vals)
		}
	}

	pts := rb.Points()
	if len(pts) != 3 || pts[0].Value != 3.0 || pts[2].Value != 5.0 {
		t.Errorf("Points() returned unexpected elements: %v", pts)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := store.NewRingBuffer(3)
	rb.Push(time.Now(), 1.0)
	rb.Push(time.Now(), 2.0)
	rb.Clear()

	if rb.Len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", rb.Len())
	}
	if len(rb.Values()) != 0 {
		t.Fatalf("expected empty values after clear, got %v", rb.Values())
	}
}
