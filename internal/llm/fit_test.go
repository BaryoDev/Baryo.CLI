package llm

import (
	"testing"
)

func TestEstimateFit(t *testing.T) {
	hw16GB := Hardware{TotalRAM: 16 * 1024 * 1024 * 1024} // 16 GiB
	hw64GB := Hardware{TotalRAM: 64 * 1024 * 1024 * 1024} // 64 GiB

	tests := []struct {
		name    string
		model   Model
		hw      Hardware
		wantTag FitTag
	}{
		{
			name:    "small model on big hardware",
			model:   Model{Size: "4.07 GiB"},
			hw:      hw64GB,
			wantTag: FitFast,
		},
		{
			name:    "medium model on 16GB",
			model:   Model{Size: "10 GiB"},
			hw:      hw16GB,
			wantTag: FitSmooth,
		},
		{
			name:    "tight model on 16GB",
			model:   Model{Size: "14 GiB"},
			hw:      hw16GB,
			wantTag: FitSlow,
		},
		{
			name:    "too large model",
			model:   Model{Size: "16 GiB"},
			hw:      hw16GB,
			wantTag: FitTooLarge,
		},
		{
			name:    "cloud model returns zero",
			model:   Model{Provider: "openai"},
			hw:      hw16GB,
			wantTag: "",
		},
		{
			name:    "no size info returns zero",
			model:   Model{},
			hw:      hw16GB,
			wantTag: "",
		},
		{
			name:    "zero RAM returns zero",
			model:   Model{Size: "4 GiB"},
			hw:      Hardware{},
			wantTag: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateFit(tt.model, tt.hw)
			if result.Tag != tt.wantTag {
				t.Errorf("EstimateFit().Tag = %q, want %q (MemoryGB=%.2f)", result.Tag, tt.wantTag, result.MemoryGB)
			}
		})
	}
}

func TestEstimateFitFromSize(t *testing.T) {
	hw := Hardware{TotalRAM: 16 * 1024 * 1024 * 1024}

	tests := []struct {
		name      string
		sizeBytes int64
		wantTag   FitTag
	}{
		{"fast fit", 4 * 1024 * 1024 * 1024, FitFast},
		{"smooth fit", 11 * 1024 * 1024 * 1024, FitSmooth},
		{"slow fit", 14 * 1024 * 1024 * 1024, FitSlow},
		{"too large", 16 * 1024 * 1024 * 1024, FitTooLarge},
		{"zero size", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateFitFromSize(tt.sizeBytes, hw)
			if result.Tag != tt.wantTag {
				t.Errorf("EstimateFitFromSize().Tag = %q, want %q", result.Tag, tt.wantTag)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"4.07 GiB", 4370055168},
		{"2048 MiB", 2147483648},
		{"1 GB", 1000000000},
		{"500 MB", 500000000},
		{"", 0},
		{"  ", 0},
		{"invalid", 0},
		{"4.07", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSize(tt.input)
			// Allow 1% tolerance for floating point
			if got == 0 && tt.want == 0 {
				return
			}
			diff := float64(got) - float64(tt.want)
			if diff < 0 {
				diff = -diff
			}
			tolerance := float64(tt.want) * 0.01
			if diff > tolerance {
				t.Errorf("parseSize(%q) = %d, want %d (diff=%f)", tt.input, got, tt.want, diff)
			}
		})
	}
}

func TestParseParams(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"7.25 B", 7.25e9},
		{"70 B", 70e9},
		{"500 M", 500e6},
		{"", 0},
		{"unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseParams(tt.input)
			if got == 0 && tt.want == 0 {
				return
			}
			diff := got - tt.want
			if diff < 0 {
				diff = -diff
			}
			tolerance := tt.want * 0.01
			if diff > tolerance {
				t.Errorf("parseParams(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestEstimateMemory(t *testing.T) {
	// Model with size string
	m1 := Model{Size: "4 GiB"}
	if estimateMemory(m1) == 0 {
		t.Error("estimateMemory with Size string should be non-zero")
	}

	// Model with params only (falls back to BPP estimate)
	m2 := Model{Params: "7 B"}
	if estimateMemory(m2) == 0 {
		t.Error("estimateMemory with Params should be non-zero")
	}

	// Model with no info
	m3 := Model{}
	if estimateMemory(m3) != 0 {
		t.Error("estimateMemory with no info should be zero")
	}

	// Model with both (Size takes precedence)
	m4 := Model{Size: "4 GiB", Params: "7 B"}
	sizeEst := estimateMemory(m4)
	m5 := Model{Size: "4 GiB"}
	sizeOnlyEst := estimateMemory(m5)
	if sizeEst != sizeOnlyEst {
		t.Error("Size should take precedence over Params")
	}
}
