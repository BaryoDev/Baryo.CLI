// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import (
	"fmt"
	"strings"
)

// FitTag classifies how well a model fits the hardware.
type FitTag string

const (
	FitFast     FitTag = "fast"
	FitSmooth   FitTag = "smooth"
	FitSlow     FitTag = "slow"
	FitTooLarge FitTag = "too large"
)

// FitResult holds the fit classification for a model on given hardware.
type FitResult struct {
	Tag      FitTag
	Remark   string
	MemoryGB float64 // estimated runtime memory in GiB
}

// EstimateFit scores how well a model fits the available hardware.
// Returns a zero FitResult for cloud/remote models (Provider != "").
func EstimateFit(model Model, hw Hardware) FitResult {
	if model.Provider != "" {
		return FitResult{}
	}

	memBytes := estimateMemory(model)
	if memBytes == 0 || hw.TotalRAM == 0 {
		return FitResult{}
	}

	memGB := float64(memBytes) / (1024 * 1024 * 1024)
	ratio := float64(memBytes) / float64(hw.TotalRAM)

	var tag FitTag
	var remark string
	switch {
	case ratio < 0.60:
		tag = FitFast
		remark = "plenty of room, expect fast responses"
	case ratio < 0.80:
		tag = FitSmooth
		remark = "fits well, may slow during long contexts"
	case ratio < 0.95:
		tag = FitSlow
		remark = "tight fit, expect slower responses and possible swapping"
	default:
		tag = FitTooLarge
		remark = "likely won't fit, expect crashes or heavy swapping"
	}

	return FitResult{Tag: tag, Remark: remark, MemoryGB: memGB}
}

// EstimateFitFromSize scores how well a model fits the available hardware
// using raw byte size instead of a Model struct. Useful for Docker Hub tags
// where only the download size is known.
func EstimateFitFromSize(sizeBytes int64, hw Hardware) FitResult {
	if sizeBytes == 0 || hw.TotalRAM == 0 {
		return FitResult{}
	}

	memGB := float64(sizeBytes) / (1024 * 1024 * 1024)
	ratio := float64(sizeBytes) / float64(hw.TotalRAM)

	var tag FitTag
	var remark string
	switch {
	case ratio < 0.60:
		tag = FitFast
		remark = "plenty of room, expect fast responses"
	case ratio < 0.80:
		tag = FitSmooth
		remark = "fits well, may slow during long contexts"
	case ratio < 0.95:
		tag = FitSlow
		remark = "tight fit, expect slower responses and possible swapping"
	default:
		tag = FitTooLarge
		remark = "likely won't fit, expect crashes or heavy swapping"
	}

	return FitResult{Tag: tag, Remark: remark, MemoryGB: memGB}
}

// estimateMemory returns estimated runtime memory in bytes.
// Primary: parse the Size field (on-disk weight ≈ runtime memory).
// Fallback: parse Params and estimate via Q4_K_M BPP.
func estimateMemory(m Model) uint64 {
	if b := parseSize(m.Size); b > 0 {
		return b
	}
	if params := parseParams(m.Params); params > 0 {
		// Q4_K_M ≈ 0.58 bytes per parameter + 0.5 GiB overhead.
		const bpp = 0.58
		const overhead = 512 * 1024 * 1024 // 0.5 GiB
		return uint64(params*bpp) + overhead
	}
	return 0
}

// parseSize parses strings like "4.07 GiB" or "2048 MiB" into bytes.
func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	upper := strings.ToUpper(s)

	var numStr string
	var multiplier float64

	switch {
	case strings.HasSuffix(upper, " GIB"):
		numStr = strings.TrimSuffix(upper, " GIB")
		multiplier = 1024 * 1024 * 1024
	case strings.HasSuffix(upper, " GB"):
		numStr = strings.TrimSuffix(upper, " GB")
		multiplier = 1_000_000_000
	case strings.HasSuffix(upper, " MIB"):
		numStr = strings.TrimSuffix(upper, " MIB")
		multiplier = 1024 * 1024
	case strings.HasSuffix(upper, " MB"):
		numStr = strings.TrimSuffix(upper, " MB")
		multiplier = 1_000_000
	default:
		return 0
	}

	numStr = strings.TrimSpace(numStr)
	var val float64
	if _, err := fmt.Sscanf(numStr, "%f", &val); err != nil {
		return 0
	}
	return uint64(val * multiplier)
}

// parseParams parses strings like "7.25 B" into a count of parameters.
func parseParams(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	upper := strings.ToUpper(s)

	var numStr string
	var multiplier float64

	switch {
	case strings.HasSuffix(upper, " B"):
		numStr = strings.TrimSuffix(upper, " B")
		multiplier = 1e9
	case strings.HasSuffix(upper, " M"):
		numStr = strings.TrimSuffix(upper, " M")
		multiplier = 1e6
	default:
		return 0
	}

	numStr = strings.TrimSpace(numStr)
	var val float64
	if _, err := fmt.Sscanf(numStr, "%f", &val); err != nil {
		return 0
	}
	return val * multiplier
}
