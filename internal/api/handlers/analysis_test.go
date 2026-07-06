package handlers

import (
	"testing"
)

func TestParseSemanticMinScore(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want float64
	}{
		{name: "empty uses default", raw: "", want: defaultSemanticMinScore},
		{name: "invalid uses default", raw: "nope", want: defaultSemanticMinScore},
		{name: "below range uses default", raw: "-0.1", want: defaultSemanticMinScore},
		{name: "above range uses default", raw: "1.1", want: defaultSemanticMinScore},
		{name: "valid zero", raw: "0", want: 0},
		{name: "valid score", raw: "0.72", want: 0.72},
		{name: "valid one", raw: "1", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseSemanticMinScore(tt.raw); got != tt.want {
				t.Fatalf("parseSemanticMinScore(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
