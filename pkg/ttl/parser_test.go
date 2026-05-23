package ttl_test

import (
	"testing"
	"time"

	"github.com/grootatwork/blinkns/pkg/ttl"
)

func TestParseTTL(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"10m", 10 * time.Minute, false},
		{"1h", time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"15h", 15 * time.Hour, false},
		{"189h", 189 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"20d", 20 * 24 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		{"3w", 21 * 24 * time.Hour, false},
		{"1mo", 30 * 24 * time.Hour, false},
		{"6mo", 180 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		// invalid
		{"0m", 0, true},    // below minimum
		{"59s", 0, true},   // unsupported unit
		{"abc", 0, true},   // garbage
		{"", 0, true},      // empty
		{"9000d", 0, true}, // exceeds 1 year
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ttl.ParseTTL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseTTL(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseTTL(%q) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("ParseTTL(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
