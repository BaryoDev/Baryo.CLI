// SPDX-License-Identifier: MIT

package llm

import (
	"math"
	"testing"
)

func TestToInt32(t *testing.T) {
	tests := []struct {
		name   string
		in     int
		want   int32
		wantOK bool
	}{
		{"zero", 0, 0, true},
		{"typical", 4096, 4096, true},
		{"max int32", math.MaxInt32, math.MaxInt32, true},
		{"min int32", math.MinInt32, math.MinInt32, true},
		{"above max int32", math.MaxInt32 + 1, 0, false},
		{"below min int32", math.MinInt32 - 1, 0, false},
		{"very large", math.MaxInt64, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toInt32(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("toInt32(%d) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
