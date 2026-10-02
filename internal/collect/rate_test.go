package collect_test

import (
	"testing"

	"github.com/coulof/kvtop/internal/collect"
)

func TestComputeRate(t *testing.T) {
	tests := []struct {
		name          string
		curr          float64
		prev          float64
		dt            float64
		expectedRate  float64
		expectedReset bool
	}{
		{
			name:          "normal increment 2s",
			curr:          104.0,
			prev:          100.0,
			dt:            2.0,
			expectedRate:  2.0,
			expectedReset: false,
		},
		{
			name:          "zero delta (cached scrape)",
			curr:          100.0,
			prev:          100.0,
			dt:            2.0,
			expectedRate:  0.0,
			expectedReset: false,
		},
		{
			name:          "counter reset (VM reboot / migration)",
			curr:          10.0,
			prev:          100.0,
			dt:            2.0,
			expectedRate:  0.0,
			expectedReset: true,
		},
		{
			name:          "zero elapsed time",
			curr:          105.0,
			prev:          100.0,
			dt:            0.0,
			expectedRate:  0.0,
			expectedReset: true,
		},
		{
			name:          "negative elapsed time",
			curr:          105.0,
			prev:          100.0,
			dt:            -1.0,
			expectedRate:  0.0,
			expectedReset: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, reset := collect.ComputeRate(tt.curr, tt.prev, tt.dt)
			if reset != tt.expectedReset {
				t.Errorf("ComputeRate() reset = %v, want %v", reset, tt.expectedReset)
			}
			if rate != tt.expectedRate {
				t.Errorf("ComputeRate() rate = %v, want %v", rate, tt.expectedRate)
			}
		})
	}
}

func TestComputeUintRate(t *testing.T) {
	tests := []struct {
		name          string
		curr          uint64
		prev          uint64
		dt            float64
		expectedRate  float64
		expectedReset bool
	}{
		{
			name:          "normal network traffic",
			curr:          2000,
			prev:          1000,
			dt:            2.0,
			expectedRate:  500.0,
			expectedReset: false,
		},
		{
			name:          "counter reset",
			curr:          500,
			prev:          1000,
			dt:            2.0,
			expectedRate:  0.0,
			expectedReset: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, reset := collect.ComputeUintRate(tt.curr, tt.prev, tt.dt)
			if reset != tt.expectedReset {
				t.Errorf("ComputeUintRate() reset = %v, want %v", reset, tt.expectedReset)
			}
			if rate != tt.expectedRate {
				t.Errorf("ComputeUintRate() rate = %v, want %v", rate, tt.expectedRate)
			}
		})
	}
}
